package catalog

import (
	"context"
	"errors"
	"fmt"
)

type Syncer struct {
	Repository Repository
	BatchSize  int
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
	fail := func(cause error) (Completion, error) {
		if failErr := s.Repository.FailRun(context.WithoutCancel(ctx), runID, cause); failErr != nil {
			return completion, errors.Join(cause, failErr)
		}
		return completion, cause
	}

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
	if fetch.CompleteSnapshot && !fetch.NotModified && emittedCount == 0 {
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
