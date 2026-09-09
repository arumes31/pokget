package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"pokget/internal/catalog"
)

type CatalogImageProcessor interface {
	// Process must support concurrent calls when worker concurrency exceeds one.
	Process(context.Context, catalog.ImageJob) (catalog.ReadyImage, error)
}

type CatalogImageWorkerConfig struct {
	Owner         string
	BatchSize     int
	Concurrency   int
	LeaseDuration time.Duration
	PollInterval  time.Duration
	MaxAttempts   int
	RetryBase     time.Duration
	RetryMaximum  time.Duration
	Now           func() time.Time
	OnChanged     func(int)
}

type CatalogImageWorker struct {
	queue         catalog.ImageQueue
	processor     CatalogImageProcessor
	owner         string
	batchSize     int
	concurrency   int
	leaseDuration time.Duration
	pollInterval  time.Duration
	maxAttempts   int
	retryBase     time.Duration
	retryMaximum  time.Duration
	now           func() time.Time
	onChanged     func(int)
}

func NewCatalogImageWorker(
	queue catalog.ImageQueue,
	processor CatalogImageProcessor,
	config CatalogImageWorkerConfig,
) (*CatalogImageWorker, error) {
	if queue == nil {
		return nil, fmt.Errorf("catalog image worker: queue is required")
	}
	if processor == nil {
		return nil, fmt.Errorf("catalog image worker: processor is required")
	}
	if config.Owner == "" {
		return nil, fmt.Errorf("catalog image worker: owner is required")
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 8
	}
	if config.Concurrency == 0 {
		config.Concurrency = 4
	}
	if config.Concurrency < 1 || config.Concurrency > 8 {
		return nil, fmt.Errorf("catalog image worker: concurrency must be between 1 and 8")
	}
	// Avoid leasing a long backlog whose leases expire before processing starts.
	config.BatchSize = min(config.BatchSize, 2*config.Concurrency)
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 5 * time.Second
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 8
	}
	if config.RetryBase <= 0 {
		config.RetryBase = 30 * time.Second
	}
	if config.RetryMaximum <= 0 {
		config.RetryMaximum = 6 * time.Hour
	}
	if config.RetryMaximum < config.RetryBase {
		return nil, fmt.Errorf("catalog image worker: retry maximum cannot be shorter than retry base")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &CatalogImageWorker{
		queue:         queue,
		processor:     processor,
		owner:         config.Owner,
		batchSize:     config.BatchSize,
		concurrency:   config.Concurrency,
		leaseDuration: config.LeaseDuration,
		pollInterval:  config.PollInterval,
		maxAttempts:   config.MaxAttempts,
		retryBase:     config.RetryBase,
		retryMaximum:  config.RetryMaximum,
		now:           config.Now,
		onChanged:     config.OnChanged,
	}, nil
}

func (w *CatalogImageWorker) Run(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("catalog image worker: worker is nil")
	}
	for {
		processed, err := w.RunOnce(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			slog.Error("Catalog image worker cycle failed", "error", err)
		}
		if err == nil && processed > 0 {
			continue
		}

		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *CatalogImageWorker) RunOnce(ctx context.Context) (int, error) {
	if w == nil || w.queue == nil || w.processor == nil {
		return 0, fmt.Errorf("catalog image worker: worker is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	started := time.Now()
	jobs, err := w.queue.LeaseImageJobs(ctx, w.owner, w.batchSize, w.leaseDuration)
	if err != nil {
		return 0, fmt.Errorf("catalog image worker: leasing jobs: %w", err)
	}
	leaseDuration := time.Since(started)
	if len(jobs) == 0 {
		return 0, ctx.Err()
	}
	slog.Debug("Catalog image batch started", "leased", len(jobs), "concurrency", w.concurrency)

	processed := 0
	readyCount := 0
	var processDuration, persistDuration time.Duration
	var persistMu sync.Mutex
	cycleErrors := make([]error, len(jobs))
	var group sync.WaitGroup
	// Acquire before spawning so both goroutines and retained images stay bounded.
	slots := make(chan struct{}, w.concurrency)
dispatch:
	for index, job := range jobs {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}
		group.Go(func() {
			defer func() { <-slots }()
			if err := ctx.Err(); err != nil {
				cycleErrors[index] = err
				return
			}
			processStarted := time.Now()
			ready, processErr := w.processor.Process(ctx, job)
			elapsed := time.Since(processStarted)
			// Publish each result promptly, but serialize database writes and counters.
			persistMu.Lock()
			defer persistMu.Unlock()
			processDuration += elapsed
			if err := ctx.Err(); err != nil {
				cycleErrors[index] = err
				return
			}
			persistStarted := time.Now()
			cycleErrors[index] = w.recordOutcome(ctx, job, ready, processErr)
			persistDuration += time.Since(persistStarted)
			if cycleErrors[index] == nil {
				processed++
				if processErr == nil {
					readyCount++
				}
			}
		})
	}
	group.Wait()
	if processed > 0 && w.onChanged != nil {
		w.onChanged(processed)
	}
	slog.Info("Catalog image batch finished", "leased", len(jobs), "concurrency", w.concurrency,
		"ready", readyCount, "failed", processed-readyCount, "uncommitted", len(jobs)-processed,
		"duration_ms", time.Since(started).Milliseconds(), "lease_ms", leaseDuration.Milliseconds(),
		"process_total_ms", processDuration.Milliseconds(), "persist_total_ms", persistDuration.Milliseconds())
	return processed, errors.Join(append(cycleErrors, ctx.Err())...)
}

func (w *CatalogImageWorker) recordOutcome(ctx context.Context, job catalog.ImageJob, ready catalog.ReadyImage, processErr error) error {
	if processErr == nil {
		ready.ImageID = job.ID
		ready.LeaseOwner = w.owner
		if err := w.queue.MarkImageReady(ctx, ready); err != nil {
			return fmt.Errorf("image %d ready: %w", job.ID, err)
		}
		return nil
	}
	failure := catalog.ImageFailure{
		ImageID: job.ID, LeaseOwner: w.owner,
		Kind: catalog.ClassifyImageProcessError(processErr), Cause: processErr,
	}
	if failure.Kind == catalog.ImageFailureRetryable {
		if job.Attempts >= w.maxAttempts {
			failure.Kind = catalog.ImageFailurePermanent
		} else {
			retryAt := w.now().Add(catalog.RetryDelay(job.Attempts, w.retryBase, w.retryMaximum))
			failure.RetryAt = &retryAt
		}
	}
	if err := w.queue.MarkImageFailed(ctx, failure); err != nil {
		return fmt.Errorf("image %d failed: %w", job.ID, err)
	}
	return nil
}
