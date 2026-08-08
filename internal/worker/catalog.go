package worker

import (
	"context"
	"log/slog"
	"time"

	"pokget/internal/catalog"
)

type CatalogStateRepository interface {
	catalog.Repository
	SourceState(context.Context, string) (catalog.SourceState, error)
}

type CatalogWorker struct {
	repository CatalogStateRepository
	providers  []catalog.Provider
	syncer     *catalog.Syncer
	interval   time.Duration
	OnChanged  func()
}

func NewCatalogWorker(repository CatalogStateRepository, providers []catalog.Provider, batchSize int, interval time.Duration) *CatalogWorker {
	return &CatalogWorker{
		repository: repository,
		providers:  append([]catalog.Provider(nil), providers...),
		syncer:     &catalog.Syncer{Repository: repository, BatchSize: batchSize},
		interval:   interval,
	}
}

func (w *CatalogWorker) Start(ctx context.Context) {
	if w == nil || w.repository == nil || len(w.providers) == 0 {
		return
	}
	interval := w.interval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.syncAll(ctx)
	for {
		select {
		case <-ctx.Done():
			slog.Info("Catalog worker stopping")
			return
		case <-ticker.C:
			w.syncAll(ctx)
		}
	}
}

func (w *CatalogWorker) syncAll(ctx context.Context) {
	changed := false
	for _, provider := range w.providers {
		if ctx.Err() != nil {
			return
		}
		state, err := w.repository.SourceState(ctx, provider.ID())
		if err != nil {
			slog.Error("Catalog: failed to load source state", "source", provider.ID(), "error", err)
			continue
		}
		mode := catalog.SyncModeIncremental
		if state.LastFullSyncAt == nil {
			mode = catalog.SyncModeFull
		}
		startedAt := time.Now()
		slog.Info("Catalog: source sync starting",
			"source", provider.ID(),
			"game", provider.Game(),
			"mode", mode,
			"previous_records", state.LastRecordCount,
		)
		completion, err := w.syncer.Sync(ctx, provider, mode, state.FetchRequest(mode))
		if err != nil {
			slog.Error("Catalog: source sync failed",
				"source", provider.ID(),
				"game", provider.Game(),
				"mode", mode,
				"previous_records", state.LastRecordCount,
				"duration", time.Since(startedAt).Round(time.Millisecond),
				"error", err,
			)
			continue
		}
		if completion.Changes.CardsInserted > 0 || completion.Changes.CardsUpdated > 0 ||
			completion.Changes.CardsDeactivated > 0 || completion.Changes.ImagesInserted > 0 ||
			completion.Changes.ImagesUpdated > 0 {
			changed = true
		}
		slog.Info("Catalog: source sync complete",
			"source", provider.ID(),
			"game", provider.Game(),
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
	if changed && w.OnChanged != nil {
		w.OnChanged()
	}
}

func syncedRecordCount(state catalog.SourceState, completion catalog.Completion) int64 {
	if completion.Fetch.NotModified {
		return state.LastRecordCount
	}
	return completion.Fetch.Count
}
