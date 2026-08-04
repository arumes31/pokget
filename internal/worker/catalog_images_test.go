package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"pokget/internal/catalog"
)

type imageQueueStub struct {
	jobs     []catalog.ImageJob
	ready    []catalog.ReadyImage
	failures []catalog.ImageFailure
}

func (q *imageQueueStub) LeaseImageJobs(context.Context, string, int, time.Duration) ([]catalog.ImageJob, error) {
	return append([]catalog.ImageJob(nil), q.jobs...), nil
}

func (q *imageQueueStub) MarkImageReady(_ context.Context, ready catalog.ReadyImage) error {
	q.ready = append(q.ready, ready)
	return nil
}

func (q *imageQueueStub) MarkImageFailed(_ context.Context, failure catalog.ImageFailure) error {
	q.failures = append(q.failures, failure)
	return nil
}

type imageProcessorStub struct{}

func (imageProcessorStub) Process(_ context.Context, job catalog.ImageJob) (catalog.ReadyImage, error) {
	switch job.ID {
	case 1:
		return catalog.ReadyImage{
			LocalPath: "one.png", ContentSHA256: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			MIMEType: "image/png", Width: 1, Height: 1, ByteSize: 1,
		}, nil
	case 2:
		return catalog.ReadyImage{}, catalog.NewImageProcessError(catalog.ImageFailureUnavailable, errors.New("not found"))
	default:
		return catalog.ReadyImage{}, catalog.NewImageProcessError(catalog.ImageFailureRetryable, errors.New("upstream unavailable"))
	}
}

func TestCatalogImageWorkerRunOnceRecordsOutcomes(t *testing.T) {
	t.Parallel()

	queue := &imageQueueStub{jobs: []catalog.ImageJob{
		{ID: 1, Attempts: 1},
		{ID: 2, Attempts: 1},
		{ID: 3, Attempts: 2},
	}}
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	changed := 0
	worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{
		Owner: "worker-a", RetryBase: time.Minute, RetryMaximum: time.Hour, Now: func() time.Time { return now },
		OnChanged: func(count int) { changed += count },
	})
	if err != nil {
		t.Fatal(err)
	}

	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if processed != 3 || len(queue.ready) != 1 || len(queue.failures) != 2 {
		t.Fatalf("processed/ready/failures = %d/%d/%d", processed, len(queue.ready), len(queue.failures))
	}
	if changed != processed {
		t.Fatalf("OnChanged count = %d, want %d", changed, processed)
	}
	if queue.ready[0].ImageID != 1 || queue.ready[0].LeaseOwner != "worker-a" {
		t.Fatalf("ready result = %+v", queue.ready[0])
	}
	if queue.failures[0].Kind != catalog.ImageFailureUnavailable || queue.failures[0].RetryAt != nil {
		t.Fatalf("unavailable failure = %+v", queue.failures[0])
	}
	if queue.failures[1].Kind != catalog.ImageFailureRetryable || queue.failures[1].RetryAt == nil {
		t.Fatalf("retryable failure = %+v", queue.failures[1])
	}
	wantRetry := now.Add(2 * time.Minute)
	if !queue.failures[1].RetryAt.Equal(wantRetry) {
		t.Fatalf("retry time = %s, want %s", queue.failures[1].RetryAt, wantRetry)
	}
}

func TestCatalogImageWorkerStopsRetryingAtAttemptLimit(t *testing.T) {
	t.Parallel()

	queue := &imageQueueStub{jobs: []catalog.ImageJob{{ID: 3, Attempts: 4}}}
	worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{
		Owner: "worker-a", MaxAttempts: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queue.failures) != 1 || queue.failures[0].Kind != catalog.ImageFailurePermanent || queue.failures[0].RetryAt != nil {
		t.Fatalf("failure = %+v", queue.failures)
	}
}

func TestCatalogImageWorkerRunOnceHonorsCancellation(t *testing.T) {
	t.Parallel()

	queue := &imageQueueStub{jobs: []catalog.ImageJob{{ID: 1}}}
	worker, err := NewCatalogImageWorker(queue, imageProcessorStub{}, CatalogImageWorkerConfig{Owner: "worker-a"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = worker.RunOnce(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce() error = %v, want context.Canceled", err)
	}
}
