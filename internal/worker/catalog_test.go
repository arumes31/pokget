package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"pokget/internal/catalog"
)

type catalogWorkerProvider struct {
	id   string
	game catalog.Game
}

func (p catalogWorkerProvider) ID() string {
	if p.id != "" {
		return p.id
	}
	return "source"
}

func (p catalogWorkerProvider) Game() catalog.Game {
	if p.game != "" {
		return p.game
	}
	return catalog.GamePokemon
}

func (p catalogWorkerProvider) Fetch(_ context.Context, _ catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	if err := emit(catalog.CardRecord{
		SourceCardID: "one",
		Name:         "Card One",
		SetName:      "Test Set",
		Language:     "en",
	}); err != nil {
		return catalog.FetchResult{}, err
	}
	return catalog.FetchResult{Count: 1, CompleteSnapshot: true}, nil
}

type catalogWorkerRepository struct {
	mu        sync.Mutex
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
	r.mu.Lock()
	r.completed++
	r.mu.Unlock()
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
	repository.mu.Lock()
	completed := repository.completed
	repository.mu.Unlock()
	if completed < 1 {
		t.Fatalf("completed syncs = %d, want at least 1", completed)
	}
}

func TestSyncedRecordCount(t *testing.T) {
	tests := []struct {
		name       string
		state      catalog.SourceState
		completion catalog.Completion
		want       int64
	}{
		{
			name:       "modified response uses fetched count",
			state:      catalog.SourceState{LastRecordCount: 100},
			completion: catalog.Completion{Fetch: catalog.FetchResult{Count: 125}},
			want:       125,
		},
		{
			name:       "not modified response retains prior count",
			state:      catalog.SourceState{LastRecordCount: 100},
			completion: catalog.Completion{Fetch: catalog.FetchResult{NotModified: true}},
			want:       100,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := syncedRecordCount(test.state, test.completion); got != test.want {
				t.Fatalf("syncedRecordCount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCatalogWorkerPublishesEachCompletedSource(t *testing.T) {
	repository := &catalogWorkerRepository{}
	worker := NewCatalogWorker(repository, []catalog.Provider{catalogWorkerProvider{id: "pokemon"}, catalogWorkerProvider{id: "magic"}}, 10, time.Hour)
	var published []int
	worker.OnChanged = func() {
		repository.mu.Lock()
		published = append(published, repository.completed)
		repository.mu.Unlock()
	}
	worker.syncAll(context.Background())
	if len(published) != 2 || published[0] != 1 || published[1] != 2 {
		t.Fatalf("newly imported languages must be available before the next source: %v", published)
	}
}

type blockingProvider struct {
	id      string
	game    catalog.Game
	blockCh chan struct{}
}

func (p blockingProvider) ID() string         { return p.id }
func (p blockingProvider) Game() catalog.Game { return p.game }
func (p blockingProvider) Fetch(ctx context.Context, _ catalog.FetchRequest, emit func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	select {
	case <-p.blockCh:
	case <-ctx.Done():
		return catalog.FetchResult{}, ctx.Err()
	}
	if err := emit(catalog.CardRecord{
		SourceCardID: "slow_card",
		Name:         "Slow Card",
		SetName:      "Slow Set",
		Language:     "en",
	}); err != nil {
		return catalog.FetchResult{}, err
	}
	return catalog.FetchResult{Count: 1, CompleteSnapshot: true}, nil
}

func TestPerTCGWorkerQueueDoesNotBlockOtherTCGs(t *testing.T) {
	repository := &catalogWorkerRepository{}
	blockCh := make(chan struct{})
	defer close(blockCh)

	slowProvider := blockingProvider{id: "slow_tcg", game: catalog.GameMagic, blockCh: blockCh}
	fastProvider := catalogWorkerProvider{id: "fast_tcg", game: catalog.GameAltered}

	worker := NewCatalogWorker(repository, []catalog.Provider{slowProvider, fastProvider}, 10, time.Hour)

	fastCompleted := make(chan struct{}, 1)
	worker.OnChanged = func() {
		// When fast TCG completes its initial sync, it should notify immediately
		select {
		case fastCompleted <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go worker.Start(ctx)

	// Fast TCG should complete and emit onChanged even while slow_tcg is still blocked in Fetch
	select {
	case <-fastCompleted:
		// Success! The fast TCG was not blocked by the slow TCG
	case <-time.After(2 * time.Second):
		t.Fatal("fast TCG was blocked by slow TCG; independent queue failed")
	}

	// Verify fast TCG worker status
	fastWorker := worker.Worker("fast_tcg")
	if fastWorker == nil {
		t.Fatal("expected fast_tcg worker to exist")
	}
	if fastWorker.IsRunning() {
		t.Fatal("fast_tcg should be idle after completing its sync")
	}

	// Slow TCG should still be running
	slowWorker := worker.Worker("slow_tcg")
	if slowWorker == nil {
		t.Fatal("expected slow_tcg worker to exist")
	}
	if !slowWorker.IsRunning() {
		t.Fatal("slow_tcg should still be actively running")
	}
}

func TestTCGWorkerCoalescesDuplicateRequests(t *testing.T) {
	repository := &catalogWorkerRepository{}
	syncer := &catalog.Syncer{Repository: repository, BatchSize: 10}
	p := catalogWorkerProvider{id: "coalesce_tcg", game: catalog.GamePokemon}

	w := NewSourceWorker(repository, p, syncer, time.Hour, nil)

	// First enqueue returns true (accepted)
	if !w.Enqueue(catalog.SyncModeIncremental) {
		t.Fatal("initial enqueue should return true")
	}

	// Second duplicate enqueue returns false (coalesced)
	if w.Enqueue(catalog.SyncModeIncremental) {
		t.Fatal("duplicate enqueue should return false (coalesced)")
	}

	// Third enqueue with Full mode upgrades the pending mode
	if w.Enqueue(catalog.SyncModeFull) {
		t.Fatal("mode upgrade should return false (coalesced into pending)")
	}

	w.mu.Lock()
	mode := w.pendingMode
	w.mu.Unlock()

	if mode != catalog.SyncModeFull {
		t.Fatalf("expected pending mode to be upgraded to full, got %q", mode)
	}
}

type panickingProvider struct {
	id   string
	game catalog.Game
}

func (p panickingProvider) ID() string         { return p.id }
func (p panickingProvider) Game() catalog.Game { return p.game }
func (p panickingProvider) Fetch(_ context.Context, _ catalog.FetchRequest, _ func(catalog.CardRecord) error) (catalog.FetchResult, error) {
	panic("upstream network failure simulation")
}

func TestTCGWorkerPanicIsolation(t *testing.T) {
	repository := &catalogWorkerRepository{}
	panicProv := panickingProvider{id: "panic_tcg", game: catalog.GameForceOfWill}
	normalProv := catalogWorkerProvider{id: "normal_tcg", game: catalog.GameDigimon}

	worker := NewCatalogWorker(repository, []catalog.Provider{panicProv, normalProv}, 10, time.Hour)

	normalDone := make(chan struct{}, 1)
	worker.OnChanged = func() {
		select {
		case normalDone <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go worker.Start(ctx)

	// Normal worker must complete successfully despite the other worker panicking
	select {
	case <-normalDone:
		// Succeeded!
	case <-time.After(2 * time.Second):
		t.Fatal("normal worker failed to complete after peer panicked")
	}

	// Panic worker should have captured error without crashing process
	pw := worker.Worker("panic_tcg")
	if pw == nil {
		t.Fatal("panic_tcg worker missing")
	}
	pw.mu.Lock()
	lastErr := pw.lastError
	pw.mu.Unlock()
	if lastErr == nil {
		t.Fatal("expected panic to be captured in lastError")
	}
}

func TestCatalogWorkerEnqueueHelpers(t *testing.T) {
	repository := &catalogWorkerRepository{}
	p1 := catalogWorkerProvider{id: "tcg_a", game: catalog.GameOnePiece}
	p2 := catalogWorkerProvider{id: "tcg_b", game: catalog.GameOnePiece}
	p3 := catalogWorkerProvider{id: "tcg_c", game: catalog.GameLorcana}

	cw := NewCatalogWorker(repository, []catalog.Provider{p1, p2, p3}, 10, time.Hour)

	// Test Enqueue non-existent
	if cw.Enqueue("invalid", catalog.SyncModeIncremental) {
		t.Fatal("expected false for invalid source")
	}

	// Test Enqueue valid
	if !cw.Enqueue("tcg_a", catalog.SyncModeIncremental) {
		t.Fatal("expected true for tcg_a")
	}

	// Test EnqueueGame
	enqueued := cw.EnqueueGame(catalog.GameOnePiece, catalog.SyncModeFull)
	// tcg_b was not enqueued yet, tcg_a was pending so coalesce upgrade returns false
	if enqueued < 1 {
		t.Fatalf("expected at least 1 enqueued for one piece, got %d", enqueued)
	}

	// Test EnqueueAll
	total := cw.EnqueueAll(catalog.SyncModeIncremental)
	// tcg_c was not yet enqueued
	if total < 1 {
		t.Fatalf("expected at least 1 enqueued for EnqueueAll, got %d", total)
	}
}
