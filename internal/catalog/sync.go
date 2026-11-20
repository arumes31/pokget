package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Syncer struct {
	Repository Repository
	BatchSize  int
}

// leaseRenewer is implemented by repositories that lease a source per run.
type leaseRenewer interface {
	RenewLease(ctx context.Context, runID string) error
}

const failRunTimeout = 10 * time.Second

// leaseRenewInterval is a variable so tests can renew quickly.
var leaseRenewInterval = RunLeaseTTL / 5

// renewLease keeps the run's lease alive until ctx ends. A lost lease cancels
// the run; a transient error is retried on the next tick while the lease lasts.
func renewLease(ctx context.Context, renewer leaseRenewer, runID string, interval time.Duration, stop context.CancelCauseFunc) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := renewer.RenewLease(ctx, runID)
			switch {
			case errors.Is(err, ErrLeaseLost):
				stop(err)
				return
			case err != nil && ctx.Err() == nil:
				slog.Warn("Catalog: renewing sync lease failed", "run", runID, "error", err)
			}
		}
	}
}

func (s *Syncer) Sync(ctx context.Context, provider Provider, mode SyncMode, request FetchRequest) (Completion, error) {
	var completion Completion
	if s.Repository == nil {
		return completion, fmt.Errorf("catalog: repository is required")
	}
	if provider == nil {
		return completion, fmt.Errorf("catalog: provider is required")
	}
	if !mode.Valid() {
		return completion, fmt.Errorf("catalog: invalid sync mode %q", mode)
	}
	batchSize := s.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	runID, err := s.Repository.BeginRun(ctx, provider.ID(), mode)
	if err != nil {
		return completion, err
	}
	parentCtx := ctx
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	if renewer, ok := s.Repository.(leaseRenewer); ok {
		go renewLease(ctx, renewer, runID, leaseRenewInterval, stop)
	}
	fail := func(cause error) (Completion, error) {
		if lost := context.Cause(ctx); errors.Is(lost, ErrLeaseLost) && !errors.Is(cause, ErrLeaseLost) {
			cause = errors.Join(cause, lost)
		}
		// A fresh, short context: on shutdown the run must still release its
		// lease, but must not hold up the exit.
		failCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), failRunTimeout)
		defer cancel()
		if failErr := s.Repository.FailRun(failCtx, runID, cause); failErr != nil {
			return completion, errors.Join(cause, failErr)
		}
		return completion, cause
	}
	// A panicking provider must not leave the run open and the source leased.
	defer func() {
		if r := recover(); r != nil {
			_, _ = fail(fmt.Errorf("catalog: source %q panicked: %v", provider.ID(), r))
			panic(r)
		}
	}()

	request.Mode = mode
	records := make([]CardRecord, 0, batchSize)
	var emittedCount int64
	flush := func() error {
		if len(records) == 0 {
			return nil
		}
		changes, err := s.Repository.UpsertBatch(ctx, Batch{
			RunID:    runID,
			SourceID: provider.ID(),
			Game:     provider.Game(),
			Records:  records,
		})
		if err != nil {
			return err
		}
		completion.Changes.Add(changes)
		records = records[:0]
		return nil
	}

	fetch, err := provider.Fetch(ctx, request, func(record CardRecord) error {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("catalog: source %q emitted invalid record %d: %w", provider.ID(), emittedCount+1, err)
		}
		emittedCount++
		records = append(records, record)
		if len(records) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	// A full run that read nothing failed, even when the provider marked the
	// snapshot partial because every set errored: recording it as a
	// successful full sync would stop the source from retrying a full run.
	if !fetch.NotModified && emittedCount == 0 && (fetch.CompleteSnapshot || mode == SyncModeFull) {
		return fail(fmt.Errorf("%w: %q", ErrEmptySnapshot, provider.ID()))
	}
	if fetch.NotModified && emittedCount != 0 {
		return fail(fmt.Errorf(
			"%w: source %q emitted %d records for a not-modified response",
			ErrRecordCountMismatch,
			provider.ID(),
			emittedCount,
		))
	}
	if !fetch.NotModified && fetch.Count != emittedCount {
		return fail(fmt.Errorf(
			"%w: source %q emitted %d records but reported %d",
			ErrRecordCountMismatch,
			provider.ID(),
			emittedCount,
			fetch.Count,
		))
	}
	completion.Fetch = fetch
	if err := flush(); err != nil {
		return fail(err)
	}
	changes, err := s.Repository.CompleteRun(ctx, runID, completion)
	if err != nil {
		return fail(err)
	}
	completion.Changes = changes
	return completion, nil
}
