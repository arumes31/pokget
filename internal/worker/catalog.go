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

type CatalogStateRepository interface {
	catalog.Repository
	SourceState(context.Context, string) (catalog.SourceState, error)
}

// SourceWorker manages the synchronization lifecycle and dedicated queue for a single TCG source.
// Each TCG source has its own isolated worker goroutine, dedicated job queue, and independent schedule,
// ensuring that slow or long-running syncs for one TCG never block or impede syncs for other TCGs.
type SourceWorker struct {
	provider catalog.Provider
	syncer   *catalog.Syncer
	repo     CatalogStateRepository
	interval time.Duration

	mu sync.Mutex

	onChanged func()

	lastStartedAt   time.Time
	lastCompletedAt time.Time
	lastError       error
}

// NewSourceWorker creates a dedicated worker and queue for a single TCG provider.
func NewSourceWorker(
	repo CatalogStateRepository,
	provider catalog.Provider,
	syncer *catalog.Syncer,
	interval time.Duration,
	onChanged func(),
) *SourceWorker {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &SourceWorker{
		provider:  provider,
		syncer:    syncer,
		repo:      repo,
		interval:  interval,
		onChanged: onChanged,
	}
}

// Run syncs this TCG source on startup and then every interval until ctx is cancelled.
func (w *SourceWorker) Run(ctx context.Context, initialJitter time.Duration) {
	if initialJitter > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialJitter):
		}
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		w.executeSync(ctx)
		select {
		case <-ctx.Done():
			slog.Debug("Source worker stopping", "source", w.provider.ID(), "game", w.provider.Game())
			return
		case <-ticker.C:
		}
	}
}

func (w *SourceWorker) executeSync(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			w.mu.Lock()
			w.lastError = fmt.Errorf("panic: %v", r)
			w.lastCompletedAt = time.Now()
			w.mu.Unlock()
			slog.Error("Catalog: source worker recovered from panic",
				"source", w.provider.ID(),
				"game", w.provider.Game(),
				"panic", r,
			)
		}
	}()

	if ctx.Err() != nil {
		return
	}

	state, err := w.repo.SourceState(ctx, w.provider.ID())
	if err != nil {
		w.mu.Lock()
		w.lastError = err
		w.lastCompletedAt = time.Now()
		w.mu.Unlock()
		slog.Error("Catalog: failed to load source state", "source", w.provider.ID(), "error", err)
		return
	}

	mode := catalog.SyncModeIncremental
	if fullSyncDue(state, time.Now()) {
		mode = catalog.SyncModeFull
	}

	startedAt := time.Now()
	w.mu.Lock()
	w.lastStartedAt = startedAt
	w.mu.Unlock()

	slog.Info("Catalog: source sync starting",
		"source", w.provider.ID(),
		"game", w.provider.Game(),
		"mode", mode,
		"previous_records", state.LastRecordCount,
	)

	completion, err := w.syncer.Sync(ctx, w.provider, mode, state.FetchRequest(mode))
	if errors.Is(err, catalog.ErrSourceBusy) {
		// The catalog CLI (or another replica) is syncing this source right now.
		slog.Info("Catalog: source sync skipped, another process holds it",
			"source", w.provider.ID(), "game", w.provider.Game(), "mode", mode)
		return
	}

	w.mu.Lock()
	w.lastCompletedAt = time.Now()
	w.lastError = err
	w.mu.Unlock()

	if err != nil {
		slog.Error("Catalog: source sync failed",
			"source", w.provider.ID(),
			"game", w.provider.Game(),
			"mode", mode,
			"previous_records", state.LastRecordCount,
			"duration", time.Since(startedAt).Round(time.Millisecond),
			"error", err,
		)
		return
	}

	if completion.Changes.CardsInserted > 0 || completion.Changes.CardsUpdated > 0 ||
		completion.Changes.CardsDeactivated > 0 || completion.Changes.ImagesInserted > 0 ||
		completion.Changes.ImagesUpdated > 0 {
		if w.onChanged != nil {
			w.onChanged()
		}
	}

	slog.Info("Catalog: source sync complete",
		"source", w.provider.ID(),
		"game", w.provider.Game(),
		"mode", mode,
		"duration", time.Since(startedAt).Round(time.Millisecond),
		"records", syncedRecordCount(state, completion),
		"fetched_records", completion.Fetch.Count,
		"previous_records", state.LastRecordCount,
		"not_modified", completion.Fetch.NotModified,
		"upstream_version", completion.Fetch.UpstreamVersion,
		"cards_inserted", completion.Changes.CardsInserted,
		"cards_updated", completion.Changes.CardsUpdated,
		"printings_inserted", completion.Changes.PrintingsInserted,
		"printings_updated", completion.Changes.PrintingsUpdated,
		"images_inserted", completion.Changes.ImagesInserted,
		"images_updated", completion.Changes.ImagesUpdated,
		"cards_deactivated", completion.Changes.CardsDeactivated,
		"printings_deactivated", completion.Changes.PrintingsDeactivated,
	)
}

// CatalogWorker coordinates per-TCG source workers, ensuring independent queues and execution.
type CatalogWorker struct {
	repository CatalogStateRepository
	providers  []catalog.Provider
	syncer     *catalog.Syncer
	interval   time.Duration

	workersMu sync.RWMutex
	workers   map[string]*SourceWorker

	notifyMu  sync.Mutex
	OnChanged func()
}

func NewCatalogWorker(
	repository CatalogStateRepository,
	providers []catalog.Provider,
	batchSize int,
	interval time.Duration,
) *CatalogWorker {
	syncer := &catalog.Syncer{Repository: repository, BatchSize: batchSize}
	cw := &CatalogWorker{
		repository: repository,
		providers:  append([]catalog.Provider(nil), providers...),
		syncer:     syncer,
		interval:   interval,
		workers:    make(map[string]*SourceWorker, len(providers)),
	}

	for _, provider := range cw.providers {
		worker := NewSourceWorker(repository, provider, syncer, interval, cw.handleSourceChanged)
		cw.workers[provider.ID()] = worker
	}

	return cw
}

func (w *CatalogWorker) handleSourceChanged() {
	w.notifyMu.Lock()
	defer w.notifyMu.Unlock()
	if w.OnChanged != nil {
		w.OnChanged()
	}
}

// Start launches independent worker goroutines for every registered TCG source.
// Each TCG operates on its own dedicated queue so that syncs of one card game never block others.
func (w *CatalogWorker) Start(ctx context.Context) {
	if w == nil || w.repository == nil || len(w.providers) == 0 {
		return
	}

	var wg sync.WaitGroup
	w.workersMu.RLock()
	workers := make([]*SourceWorker, 0, len(w.workers))
	for _, provider := range w.providers {
		if worker, exists := w.workers[provider.ID()]; exists {
			workers = append(workers, worker)
		}
	}
	w.workersMu.RUnlock()

	for i, worker := range workers {
		wg.Add(1)
		// Stagger initial starts (0-1.5s) to avoid simultaneous thundering herd
		jitter := time.Duration(i*50) * time.Millisecond
		go func(sw *SourceWorker, j time.Duration) {
			defer wg.Done()
			sw.Run(ctx, j)
		}(worker, jitter)
	}

	<-ctx.Done()
	slog.Info("Catalog worker stopping")
	wg.Wait()
}

// fullSyncInterval: incremental runs never retire cards and some sources cap
// them to the newest pages, so each source attempts a full run weekly.
const fullSyncInterval = 7 * 24 * time.Hour

// fullSyncDue measures from the last full attempt, not the last complete one:
// a partial run does not advance last_full_sync_at, and a flaky source must not
// run full every cycle.
func fullSyncDue(state catalog.SourceState, now time.Time) bool {
	return state.LastFullAttemptAt == nil || now.Sub(*state.LastFullAttemptAt) >= fullSyncInterval
}

func syncedRecordCount(state catalog.SourceState, completion catalog.Completion) int64 {
	if completion.Fetch.NotModified {
		return state.LastRecordCount
	}
	return completion.Fetch.Count
}
