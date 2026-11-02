package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
		// A stalled download must not block the daily cycle forever.
		sourceCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
		err := w.applyBulkSource(sourceCtx, source)
		cancel()
		if err != nil {
			slog.Error("Bulk prices: source failed", "source", source.Name(), "error", err)
		}
	}
}

func (w *DataSyncWorker) applyBulkSource(ctx context.Context, source service.BulkPriceSource) error {
	if source == nil {
		return nil
	}
	batch := make([]service.BulkPriceUpdate, 0, bulkPriceBatchSize)
	// Fallback matches per game: normalized set code + number -> USD price.
	fuzzy := make(map[string]map[string]float64)
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
		if update.Game != "" && update.SetCode != "" && update.Number != "" && update.USD > 0 {
			prices := fuzzy[update.Game]
			if prices == nil {
				prices = make(map[string]float64)
				fuzzy[update.Game] = prices
			}
			key := priceMatchKey(update.SetCode, update.Number)
			if _, exists := prices[key]; !exists {
				prices[key] = update.USD
			}
		}
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

	var fuzzyUpdated int64
	for game, prices := range fuzzy {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed, err := w.applyFuzzyPrices(ctx, game, prices)
		if err != nil {
			slog.Warn("Bulk prices: fallback match failed", "source", source.Name(), "game", game, "error", err)
			continue
		}
		fuzzyUpdated += changed
	}

	slog.Info("Bulk prices applied", "source", source.Name(), "rows", rows, "updated", updated, "fallback_updated", fuzzyUpdated)
	return nil
}

// applyFuzzyPrices fills prices for cards that have none yet by matching the
// normalized set code and collector number. Existing prices are never replaced,
// so dedicated sources (Scryfall, YGOPRODeck) keep their values.
func (w *DataSyncWorker) applyFuzzyPrices(ctx context.Context, game string, prices map[string]float64) (int64, error) {
	if len(prices) == 0 {
		return 0, nil
	}
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, COALESCE(set_code, ''), COALESCE(collector_number, '')
		FROM cards
		WHERE game = $1
		  AND superseded_by_card_id IS NULL
		  AND (price_usd IS NULL OR price_usd <= 0)
		  AND set_code IS NOT NULL AND set_code <> ''
		  AND collector_number IS NOT NULL AND collector_number <> ''`, game)
	if err != nil {
		return 0, fmt.Errorf("fallback price query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	updates := make(map[string]float64)
	for rows.Next() {
		var id, setCode, number string
		if err := rows.Scan(&id, &setCode, &number); err != nil {
			continue
		}
		if price, ok := prices[priceMatchKey(setCode, number)]; ok {
			updates[id] = price
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var total int64
	pending := make([]string, 0, bulkPriceBatchSize)
	values := make(map[string]float64)
	flushIDs := func() error {
		if len(pending) == 0 {
			return nil
		}
		batch := make([]service.BulkPriceUpdate, 0, len(pending))
		for _, id := range pending {
			batch = append(batch, service.BulkPriceUpdate{SourceCardID: id, USD: values[id]})
		}
		changed, err := w.applyPriceByCardID(ctx, batch)
		if err != nil {
			return err
		}
		total += changed
		pending = pending[:0]
		return nil
	}
	for id, price := range updates {
		pending = append(pending, id)
		values[id] = price
		if len(pending) >= bulkPriceBatchSize {
			if err := flushIDs(); err != nil {
				return total, err
			}
		}
	}
	if err := flushIDs(); err != nil {
		return total, err
	}
	return total, nil
}

// applyPriceByCardID writes prices for explicit card ids.
func (w *DataSyncWorker) applyPriceByCardID(ctx context.Context, batch []service.BulkPriceUpdate) (int64, error) {
	if len(batch) == 0 {
		return 0, nil
	}
	var query strings.Builder
	query.WriteString(`UPDATE cards AS c
SET price_usd = v.usd, last_updated = NOW()
FROM (VALUES `)
	args := make([]any, 0, len(batch)*2)
	for index, update := range batch {
		if index > 0 {
			query.WriteString(", ")
		}
		base := index * 2
		fmt.Fprintf(&query, "($%d, $%d::numeric)", base+1, base+2)
		args = append(args, update.SourceCardID, update.USD)
	}
	query.WriteString(`) AS v(id, usd)
WHERE c.id = v.id AND (c.price_usd IS NULL OR c.price_usd IS DISTINCT FROM v.usd)`)
	result, err := w.db.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("fallback price update: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}

// priceMatchKey normalizes a set code and collector number so TCGplayer and
// catalog values compare equal ("OP-01" + "016/118" -> "op01|016").
func priceMatchKey(setCode, number string) string {
	return normalizePriceToken(setCode) + "|" + normalizePriceToken(strings.SplitN(number, "/", 2)[0])
}

func normalizePriceToken(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
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
