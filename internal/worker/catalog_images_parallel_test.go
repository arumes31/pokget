package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"pokget/internal/catalog"
)

func TestCatalogImageWorkerParallelBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var active, peak atomic.Int32
		queue := &hookImageQueue{jobs: make([]catalog.ImageJob, 8)}
		processor := hookImageProcessor{process: func(context.Context, catalog.ImageJob) (catalog.ReadyImage, error) {
			n := active.Add(1)
			defer active.Add(-1)
			for previous := peak.Load(); n > previous; previous = peak.Load() {
				if peak.CompareAndSwap(previous, n) {
					break
				}
			}
			time.Sleep(time.Second)
			return catalog.ReadyImage{}, nil
		}}
		worker, err := NewCatalogImageWorker(queue, processor, CatalogImageWorkerConfig{Owner: "test"})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		count, err := worker.RunOnce(t.Context())
		if err != nil || count != 8 {
			t.Fatalf("RunOnce = %d, %v", count, err)
		}
		if peak.Load() != 4 || active.Load() != 0 || time.Since(started) != 2*time.Second {
			t.Fatalf("peak=%d active=%d elapsed=%s; want 4, 0, 2s", peak.Load(), active.Load(), time.Since(started))
		}
	})
}

func TestCatalogImageWorkerBacklogDoesNotWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		leases := 0
		queue := &hookImageQueue{jobs: []catalog.ImageJob{{ID: 1}}}
		queue.onLease = func() {
			leases++
			if leases == 3 {
				cancel()
			}
		}
		worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{Owner: "test"})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed != 0 {
			t.Fatalf("backlogged worker waited %s between batches", elapsed)
		}
	})
}

func TestCatalogImageWorkerCancellationJoinsProcessors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var active, calls atomic.Int32
		queue := &imageQueueStub{jobs: make([]catalog.ImageJob, 8)}
		processor := hookImageProcessor{process: func(ctx context.Context, _ catalog.ImageJob) (catalog.ReadyImage, error) {
			calls.Add(1)
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			return catalog.ReadyImage{}, ctx.Err()
		}}
		worker, err := NewCatalogImageWorker(queue, processor, CatalogImageWorkerConfig{Owner: "test"})
		if err != nil {
			t.Fatal(err)
		}
		go func() { time.Sleep(time.Second); cancel() }()
		count, err := worker.RunOnce(ctx)
		if !errors.Is(err, context.Canceled) || count != 0 || len(queue.failures) != 0 {
			t.Fatalf("cancelled batch = %d, %v, failures=%d", count, err, len(queue.failures))
		}
		if active.Load() != 0 || calls.Load() != 4 {
			t.Fatalf("active=%d calls=%d", active.Load(), calls.Load())
		}
	})
}

func TestCatalogImageWorkerConcurrencyConfiguration(t *testing.T) {
	for _, concurrency := range []int{-1, 9} {
		if _, err := NewCatalogImageWorker(&hookImageQueue{}, imageProcessorStub{}, CatalogImageWorkerConfig{
			Owner: "test", Concurrency: concurrency,
		}); err == nil {
			t.Fatalf("accepted concurrency %d", concurrency)
		}
	}
	for _, concurrency := range []int{1, 2, 8} {
		worker, err := NewCatalogImageWorker(&hookImageQueue{}, imageProcessorStub{}, CatalogImageWorkerConfig{
			Owner: "test", Concurrency: concurrency, BatchSize: 1000,
		})
		if err != nil {
			t.Fatal(err)
		}
		if worker.concurrency != concurrency || worker.batchSize != 2*concurrency {
			t.Fatalf("concurrency=%d batch=%d", worker.concurrency, worker.batchSize)
		}
	}
}

func TestCatalogImageWorkerIdleAndErrorsBackOff(t *testing.T) {
	for _, leaseErr := range []error{nil, errors.New("database unavailable")} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			leases := 0
			queue := &hookImageQueue{leaseErr: leaseErr, onLease: func() {
				leases++
				if leases == 3 {
					cancel()
				}
			}}
			worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{Owner: "test"})
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if elapsed := time.Since(started); elapsed != 10*time.Second {
				t.Fatalf("idle/error backoff = %s, want 10s", elapsed)
			}
		})
	}
}
