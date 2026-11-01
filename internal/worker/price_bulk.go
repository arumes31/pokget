package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"pokget/internal/service"
)

// bulkPriceBatchSize keeps the VALUES list of one price update within a
// comfortable statement size (4 parameters per row).
const bulkPriceBatchSize = 500

// syncBulkPrices refreshes catalog prices from the free daily bulk exports
// (Scryfall for Magic, YGOPRODeck for Yu-Gi-Oh). It runs once per day and is
// coordinated through the same cycle lease as the other sync jobs.
func (w *DataSyncWorker) syncBulkPrices(ctx context.Context) {
	if len(w.bulkPriceSources) == 0 {
		return
	}
	release, acquired, err := w.acquireCycle(ctx, "bulk_prices")
	if err != nil {
		slog.Error("Bulk prices: failed to acquire cycle lease", "error", err)
		return
	}
	if !acquired {
		slog.Debug("Bulk prices: another replica owns the cycle")
		return
	}
	defer w.releaseCycle(release, "bulk_prices")

	for _, source := range w.bulkPriceSources {
		if err := ctx.Err(); err != nil {
			slog.Info("Bulk prices: stopping due to context cancellation", "error", err)
			return
		}
		if err := w.applyBulkSource(ctx, source); err != nil {
			slog.Error("Bulk prices: source failed", "source", source.Name(), "error", err)
		}
	}
}

func (w *DataSyncWorker) applyBulkSource(ctx context.Context, source service.BulkPriceSource) error {
	if source == nil {
		return nil
	}
	batch := make([]service.BulkPriceUpdate, 0, bulkPriceBatchSize)
	var rows, updated int64

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		changed, err := w.applyBulkBatch(ctx, batch)
		if err != nil {
			return err
		}
		rows += int64(len(batch))
		updated += changed
		batch = batch[:0]
		return nil
	}

	err := source.FetchPrices(ctx, func(update service.BulkPriceUpdate) error {
		batch = append(batch, update)
		if len(batch) >= bulkPriceBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	slog.Info("Bulk prices applied", "source", source.Name(), "rows", rows, "updated", updated)
	return nil
}

// applyBulkBatch writes one batch with a single statement. Only cards whose
// price actually changed are touched, so last_updated keeps meaning "price
// changed at".
func (w *DataSyncWorker) applyBulkBatch(ctx context.Context, batch []service.BulkPriceUpdate) (int64, error) {
	if len(batch) == 0 {
		return 0, nil
	}

	var query strings.Builder
	query.WriteString(`UPDATE cards AS c
SET price_usd = v.usd, price_eur = v.eur, last_updated = NOW()
FROM (VALUES `)
	args := make([]any, 0, len(batch)*4)
	for index, update := range batch {
		if index > 0 {
			query.WriteString(", ")
		}
		base := index * 4
		fmt.Fprintf(&query, "($%d, $%d, $%d::numeric, $%d::numeric)", base+1, base+2, base+3, base+4)
		args = append(args, update.SourceID, update.SourceCardID, update.USD, update.EUR)
	}
	query.WriteString(`) AS v(source_id, source_card_id, usd, eur)
WHERE c.source_id = v.source_id
  AND c.source_card_id = v.source_card_id
  AND (c.price_usd IS DISTINCT FROM v.usd OR c.price_eur IS DISTINCT FROM v.eur)`)

	result, err := w.db.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("bulk price update: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}
