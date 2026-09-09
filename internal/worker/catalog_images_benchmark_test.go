package worker

import (
	"context"
	"testing"
	"time"

	"pokget/internal/catalog"
)

// Fixed download latency isolates scheduling from upstream and disk variability.
func BenchmarkCatalogImageWorkerLatency(b *testing.B) {
	queue := &hookImageQueue{jobs: make([]catalog.ImageJob, 8)}
	processor := hookImageProcessor{process: func(ctx context.Context, _ catalog.ImageJob) (catalog.ReadyImage, error) {
		select {
		case <-time.After(10 * time.Millisecond):
			return catalog.ReadyImage{}, nil
		case <-ctx.Done():
			return catalog.ReadyImage{}, ctx.Err()
		}
	}}
	worker, err := NewCatalogImageWorker(queue, processor, CatalogImageWorkerConfig{Owner: "benchmark"})
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := worker.RunOnce(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
