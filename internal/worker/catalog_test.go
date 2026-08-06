package worker

import (
	"context"
	"testing"
	"time"

	"pokget/internal/catalog"
)

type catalogWorkerProvider struct{ id string }

func (p catalogWorkerProvider) ID() string         { return p.id }
func (p catalogWorkerProvider) Game() catalog.Game { return catalog.GamePokemon }
func (p catalogWorkerProvider) Fetch(_ context.Context, _ catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	if err := emit(catalog.CardRecord{SourceCardID: "one"}); err != nil {
		return catalog.FetchResult{}, err
	}
	return catalog.FetchResult{Count: 1, CompleteSnapshot: true}, nil
}

type catalogWorkerRepository struct {
	completed int
}

func (r *catalogWorkerRepository) SourceState(context.Context, string) (catalog.SourceState, error) {
	return catalog.SourceState{}, nil
}
func (r *catalogWorkerRepository) BeginRun(context.Context, string, catalog.SyncMode) (string, error) {
	return "run", nil
}
func (r *catalogWorkerRepository) UpsertBatch(context.Context, catalog.Batch) (catalog.ChangeCounts, error) {
	return catalog.ChangeCounts{CardsInserted: 1}, nil
}
func (r *catalogWorkerRepository) CompleteRun(context.Context, string, catalog.Completion) (catalog.ChangeCounts, error) {
	r.completed++
	return catalog.ChangeCounts{CardsInserted: 1}, nil
}
func (r *catalogWorkerRepository) FailRun(context.Context, string, error) error { return nil }

func TestCatalogWorkerInitialSyncAndCancellation(t *testing.T) {
	repository := &catalogWorkerRepository{}
	worker := NewCatalogWorker(repository, []catalog.Provider{catalogWorkerProvider{id: "source"}}, 10, time.Hour)
	changed := make(chan struct{}, 1)
	worker.OnChanged = func() { changed <- struct{}{} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Start(ctx)
		close(done)
	}()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("catalog worker did not complete initial sync")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("catalog worker did not stop after cancellation")
	}
	if repository.completed != 1 {
		t.Fatalf("completed syncs = %d, want 1", repository.completed)
	}
}
