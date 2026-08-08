package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"pokget/internal/catalog"
)

type hookImageQueue struct {
	jobs     []catalog.ImageJob
	leaseErr error
	readyErr error
	failErr  error
	onLease  func()
}

func (q *hookImageQueue) LeaseImageJobs(context.Context, string, int, time.Duration) ([]catalog.ImageJob, error) {
	if q.onLease != nil {
		q.onLease()
	}
	return q.jobs, q.leaseErr
}

func (q *hookImageQueue) MarkImageReady(context.Context, catalog.ReadyImage) error {
	return q.readyErr
}

func (q *hookImageQueue) MarkImageFailed(context.Context, catalog.ImageFailure) error {
	return q.failErr
}

type hookImageProcessor struct {
	process func(context.Context, catalog.ImageJob) (catalog.ReadyImage, error)
}

func (p hookImageProcessor) Process(ctx context.Context, job catalog.ImageJob) (catalog.ReadyImage, error) {
	return p.process(ctx, job)
}

func TestCatalogImageWorkerRun(t *testing.T) {
	t.Run("NilWorker", func(t *testing.T) {
		var worker *CatalogImageWorker
		if err := worker.Run(context.Background()); err == nil {
			t.Fatal("Run() error = nil, want nil worker error")
		}
	})

	t.Run("ReturnsContextErrorAfterFailedCycle", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		queue := &hookImageQueue{
			leaseErr: errors.New("lease down"),
			onLease:  cancel,
		}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{
			Owner: "worker-a", PollInterval: time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	})

	t.Run("LogsCycleErrorAndKeepsPolling", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		leases := 0
		queue := &hookImageQueue{
			leaseErr: errors.New("lease down"),
			onLease: func() {
				leases++
			},
		}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{
			Owner: "worker-a", PollInterval: time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Cancel while the worker waits out its poll interval after the failed
		// cycle; the run must end with the context error, not the lease error.
		time.AfterFunc(50*time.Millisecond, cancel)
		if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
		if leases != 1 {
			t.Fatalf("leases = %d, want 1 before cancellation", leases)
		}
	})

	t.Run("IdleCycleWaitsForPollInterval", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		queue := &hookImageQueue{onLease: cancel}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{
			Owner: "worker-a", PollInterval: time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	})
}

func TestCatalogImageWorkerRunOnceErrors(t *testing.T) {
	t.Parallel()

	t.Run("NilWorker", func(t *testing.T) {
		var worker *CatalogImageWorker
		if _, err := worker.RunOnce(context.Background()); err == nil {
			t.Fatal("RunOnce() error = nil, want uninitialized worker error")
		}
	})

	t.Run("LeaseError", func(t *testing.T) {
		queue := &hookImageQueue{leaseErr: errors.New("lease down")}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{Owner: "worker-a"})
		if err != nil {
			t.Fatal(err)
		}
		processed, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Fatal("RunOnce() error = nil, want lease error")
		}
		if processed != 0 {
			t.Fatalf("processed = %d, want 0", processed)
		}
	})

	t.Run("MarkReadyErrorIsReported", func(t *testing.T) {
		queue := &hookImageQueue{
			jobs:     []catalog.ImageJob{{ID: 1, Attempts: 1}},
			readyErr: errors.New("write down"),
		}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{Owner: "worker-a"})
		if err != nil {
			t.Fatal(err)
		}
		processed, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Fatal("RunOnce() error = nil, want ready-marking error")
		}
		if processed != 0 {
			t.Fatalf("processed = %d, want 0 when marking ready fails", processed)
		}
	})

	t.Run("MarkFailedErrorIsReported", func(t *testing.T) {
		queue := &hookImageQueue{
			jobs:    []catalog.ImageJob{{ID: 3, Attempts: 1}},
			failErr: errors.New("write down"),
		}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{
			Owner: "worker-a", RetryBase: time.Minute, RetryMaximum: time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		processed, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Fatal("RunOnce() error = nil, want failure-marking error")
		}
		if processed != 0 {
			t.Fatalf("processed = %d, want 0 when marking failed fails", processed)
		}
	})

	t.Run("CancellationDuringProcessingReturnsContextError", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		processor := hookImageProcessor{process: func(context.Context, catalog.ImageJob) (catalog.ReadyImage, error) {
			cancel()
			return catalog.ReadyImage{}, catalog.NewImageProcessError(catalog.ImageFailureRetryable, errors.New("boom"))
		}}
		queue := &hookImageQueue{jobs: []catalog.ImageJob{{ID: 1, Attempts: 1}}}
		worker, err := NewCatalogImageWorker(queue, processor, CatalogImageWorkerConfig{Owner: "worker-a"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := worker.RunOnce(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunOnce() error = %v, want context.Canceled", err)
		}
	})
}
