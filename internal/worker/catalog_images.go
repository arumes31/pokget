package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"pokget/internal/catalog"
)

type CatalogImageProcessor interface {
	Process(context.Context, catalog.ImageJob) (catalog.ReadyImage, error)
}

type CatalogImageWorkerConfig struct {
	Owner         string
	BatchSize     int
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
		_, err := w.RunOnce(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			slog.Error("Catalog image worker cycle failed", "error", err)
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
	jobs, err := w.queue.LeaseImageJobs(ctx, w.owner, w.batchSize, w.leaseDuration)
	if err != nil {
		return 0, fmt.Errorf("catalog image worker: leasing jobs: %w", err)
	}

	processed := 0
	var cycleErrors []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return processed, errors.Join(append(cycleErrors, err)...)
		}
		ready, processErr := w.processor.Process(ctx, job)
		if processErr == nil {
			ready.ImageID = job.ID
			ready.LeaseOwner = w.owner
			if err := w.queue.MarkImageReady(ctx, ready); err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("image %d ready: %w", job.ID, err))
				continue
			}
			processed++
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return processed, errors.Join(append(cycleErrors, ctxErr)...)
		}

		failure := catalog.ImageFailure{
			ImageID:    job.ID,
			LeaseOwner: w.owner,
			Kind:       catalog.ClassifyImageProcessError(processErr),
			Cause:      processErr,
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
			cycleErrors = append(cycleErrors, fmt.Errorf("image %d failed: %w", job.ID, err))
			continue
		}
		processed++
	}
	if processed > 0 && w.onChanged != nil {
		w.onChanged(processed)
	}
	return processed, errors.Join(cycleErrors...)
}
