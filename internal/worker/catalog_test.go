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

type modeRecordingRepository struct {
	catalogWorkerRepository
	state catalog.SourceState
	modes []catalog.SyncMode
}

func (r *modeRecordingRepository) SourceState(context.Context, string) (catalog.SourceState, error) {
	return r.state, nil
}

func (r *modeRecordingRepository) BeginRun(_ context.Context, _ string, mode catalog.SyncMode) (string, error) {
	r.modes = append(r.modes, mode)
	return "run", nil
}

// The app schedules a full run when the last full attempt is a week old, even
// after a first complete full sync; a recent attempt (also a partial one that
// did not advance last_full_sync_at) keeps the run incremental.
func TestSourceWorkerSchedulesWeeklyFullSync(t *testing.T) {
	now := time.Now()
	recent, old := now.Add(-time.Hour), now.Add(-8*24*time.Hour)
	tests := []struct {
		name  string
		state catalog.SourceState
		want  catalog.SyncMode
	}{
		{"never attempted", catalog.SourceState{}, catalog.SyncModeFull},
		{"stale full attempt", catalog.SourceState{LastFullSyncAt: &old, LastFullAttemptAt: &old}, catalog.SyncModeFull},
		{"recent complete full", catalog.SourceState{LastFullSyncAt: &recent, LastFullAttemptAt: &recent}, catalog.SyncModeIncremental},
		{"recent partial full", catalog.SourceState{LastFullAttemptAt: &recent}, catalog.SyncModeIncremental},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &modeRecordingRepository{state: test.state}
			syncer := &catalog.Syncer{Repository: repository}
			NewSourceWorker(repository, catalogWorkerProvider{}, syncer, time.Hour, nil).executeSync(context.Background())
			if len(repository.modes) != 1 || repository.modes[0] != test.want {
				t.Fatalf("modes = %v, want [%s]", repository.modes, test.want)
			}
		})
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
	pw := worker.workers["panic_tcg"]
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

type busyCatalogRepository struct{ catalogWorkerRepository }

func (r *busyCatalogRepository) BeginRun(context.Context, string, catalog.SyncMode) (string, error) {
	return "", catalog.ErrSourceBusy
}

// A source synced by the catalog CLI right now is skipped, not reported as failed.
func TestSourceWorkerSkipsBusySource(t *testing.T) {
	repository := &busyCatalogRepository{}
	syncer := &catalog.Syncer{Repository: repository}
	w := NewSourceWorker(repository, catalogWorkerProvider{}, syncer, time.Hour, nil)
	if busy := w.executeSync(context.Background()); !busy || w.lastError != nil {
		t.Fatalf("a busy source is skipped, not failed: busy=%v lastError=%v", busy, w.lastError)
	}
}
