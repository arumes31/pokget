package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"pokget/internal/service"

	"github.com/lib/pq"
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
	// Last step: give every freshly priced card an EUR value even when its
	// marketplace has none.
	w.applyEURFallback(ctx)
}

func (w *DataSyncWorker) applyBulkSource(ctx context.Context, source service.BulkPriceSource) error {
	if source == nil {
		return nil
	}
	batch := make([]service.BulkPriceUpdate, 0, bulkPriceBatchSize)
	// Fallback matches per game and language: normalized set code + number -> USD
	// price, and the same keyed by set name for games whose catalog codes differ
	// from the marketplace abbreviations. Language-bound categories (TCGplayer's
	// "Pokemon Japan") get their own scope so a Japanese price never lands on an
	// English printing.
	fuzzy := make(map[fuzzyScope]map[string]float64)
	fuzzyByName := make(map[fuzzyScope]map[string]float64)
	var rows, updated int64
	// Languages that come with their own marketplace category. The generic
	// category of the same game must skip them: TCGplayer's Japanese Pokémon sets
	// reuse English set codes for entirely different cards ("SV3" is "Ruler of the
	// Black Flame" in Japan and "Obsidian Flames" in English).
	boundLanguages := make(map[string]map[string]struct{})

	rememberKeys := func(index map[fuzzyScope]map[string]float64, scope fuzzyScope, set, number string, usd float64) {
		keys := priceMatchKeys(set, number)
		if len(keys) == 0 {
			return
		}
		prices := index[scope]
		if prices == nil {
			prices = make(map[string]float64)
			index[scope] = prices
		}
		for _, key := range keys {
			if _, exists := prices[key]; !exists {
				prices[key] = usd
			}
		}
	}

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
		if update.Game != "" && update.Number != "" && update.USD > 0 {
			scope := fuzzyScope{game: update.Game, language: update.Language}
			if update.Language != "" {
				if boundLanguages[update.Game] == nil {
					boundLanguages[update.Game] = make(map[string]struct{})
				}
				boundLanguages[update.Game][update.Language] = struct{}{}
			}
			if update.SetCode != "" {
				rememberKeys(fuzzy, scope, update.SetCode, update.Number, update.USD)
			}
			for _, alias := range setNameAliases(update.SetName) {
				rememberKeys(fuzzyByName, scope, alias, update.Number, update.USD)
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
	// Language-bound scopes run first: the Japanese Pokémon prices must win over
	// the language-agnostic pass that would otherwise settle for an English one.
	for _, scope := range orderedFuzzyScopes(fuzzy) {
		if err := ctx.Err(); err != nil {
			return err
		}
		exclude := []string(nil)
		if scope.language == "" {
			for language := range boundLanguages[scope.game] {
				exclude = append(exclude, language)
			}
			sort.Strings(exclude)
		}
		changed, err := w.applyFuzzyPricesExcluding(ctx, scope.game, scope.language, exclude, fuzzy[scope], fuzzyByName[scope])
		if err != nil {
			slog.Warn("Bulk prices: fallback match failed", "source", source.Name(), "game", scope.game, "language", scope.language, "error", err)
			continue
		}
		fuzzyUpdated += changed
	}

	slog.Info("Bulk prices applied", "source", source.Name(), "rows", rows, "updated", updated, "fallback_updated", fuzzyUpdated)
	return nil
}

// applyEURFallback fills the EUR column for cards whose marketplace only publishes
// USD prices (every TCGplayer category except Yu-Gi-Oh, plus the Japanese Pokémon
// catalog). The value is converted with the daily ECB reference rate and flagged
// as estimated, so neither the UI nor an operator mistakes it for a Cardmarket
// quote. Cards that already carry a native EUR price are never touched.
func (w *DataSyncWorker) applyEURFallback(ctx context.Context) {
	if w.eurRate == nil {
		return
	}
	rate, err := w.eurRate.USDEUR(ctx)
	if err != nil {
		slog.Warn("Price FX: no USD->EUR rate, keeping EUR prices untouched", "error", err)
		return
	}
	if rate <= 0 {
		slog.Warn("Price FX: provider returned no usable rate", "rate", rate)
		return
	}
	result, err := w.db.ExecContext(ctx, `
		UPDATE cards
		SET price_eur = ROUND(price_usd * $1, 2), price_eur_estimated = TRUE
		WHERE superseded_by_card_id IS NULL
		  AND price_usd > 0
		  AND (price_eur IS NULL OR price_eur <= 0)`, rate)
	if err != nil {
		slog.Warn("Price FX: conversion failed", "error", err)
		return
	}
	changed, err := result.RowsAffected()
	if err != nil {
		changed = 0
	}
	if changed > 0 {
		slog.Info("Price FX: derived EUR prices from USD", "rate", rate, "rows", changed)
	}
}

// fuzzyScope limits a fallback match to one game and, for marketplaces that
// split a game by language, to one language.
type fuzzyScope struct {
	game     string
	language string
}

// orderedFuzzyScopes returns the scopes of an index in a deterministic order with
// the language-bound scopes first.
func orderedFuzzyScopes(index map[fuzzyScope]map[string]float64) []fuzzyScope {
	scopes := make([]fuzzyScope, 0, len(index))
	for scope := range index {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if (scopes[i].language == "") != (scopes[j].language == "") {
			return scopes[i].language != ""
		}
		if scopes[i].game != scopes[j].game {
			return scopes[i].game < scopes[j].game
		}
		return scopes[i].language < scopes[j].language
	})
	return scopes
}

// applyFuzzyPrices fills prices for cards that have none yet by matching the
// normalized set code and collector number, then the normalized set name and
// number. Existing prices are never replaced, so dedicated sources (Scryfall,
// YGOPRODeck) keep their values.
func (w *DataSyncWorker) applyFuzzyPrices(ctx context.Context, game, language string, prices, namePrices map[string]float64) (int64, error) {
	return w.applyFuzzyPricesExcluding(ctx, game, language, nil, prices, namePrices)
}

// applyFuzzyPricesExcluding is applyFuzzyPrices with a list of languages the
// match must skip. The generic marketplace category of a game uses it to leave
// the languages alone that have their own category.
func (w *DataSyncWorker) applyFuzzyPricesExcluding(ctx context.Context, game, language string, excludeLanguages []string, prices, namePrices map[string]float64) (int64, error) {
	if len(prices) == 0 && len(namePrices) == 0 {
		return 0, nil
	}
	// One static statement keeps every value a bound parameter: $2 restricts the
	// match to one language ("" = all), $3 lists the languages to skip.
	const query = `
		SELECT id, COALESCE(set_code, ''), COALESCE(set_name, ''), COALESCE(collector_number, '')
		FROM cards
		WHERE game = $1
		  AND superseded_by_card_id IS NULL
		  AND (price_usd IS NULL OR price_usd <= 0)
		  AND collector_number IS NOT NULL AND collector_number <> ''
		  AND ($2 = '' OR language = $2)
		  AND COALESCE(language, '') <> ALL($3::text[])`
	rows, err := w.db.QueryContext(ctx, query, game, language, pq.Array(excludeLanguages))
	if err != nil {
		return 0, fmt.Errorf("fallback price query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	lookup := func(index map[string]float64, set, number string) (float64, bool) {
		for _, key := range priceMatchKeys(set, number) {
			if price, ok := index[key]; ok {
				return price, true
			}
		}
		return 0, false
	}

	updates := make(map[string]float64)
	for rows.Next() {
		var id, setCode, setName, number string
		if err := rows.Scan(&id, &setCode, &setName, &number); err != nil {
			continue
		}
		if setCode != "" {
			if price, ok := lookup(prices, setCode, number); ok {
				updates[id] = price
				continue
			}
		}
		if setName != "" && len(namePrices) > 0 {
			if price, ok := lookup(namePrices, setName, number); ok {
				updates[id] = price
			}
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

// setNameAliases returns a marketplace set name plus the bare name after the
// set-code prefix. TCGplayer writes "ME01: Mega Evolution" where the catalog
// (TCGdex) says "Mega Evolution", so both spellings must be indexed.
func setNameAliases(name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	aliases := []string{trimmed}
	if _, after, found := strings.Cut(trimmed, ":"); found {
		if bare := strings.TrimSpace(after); bare != "" {
			aliases = append(aliases, bare)
		}
	}
	return aliases
}

// applyBulkBatch writes one batch with a single statement. Only cards whose
// price actually changed are touched, so last_updated keeps meaning "price
// changed at".
func (w *DataSyncWorker) applyBulkBatch(ctx context.Context, batch []service.BulkPriceUpdate) (int64, error) {
	if len(batch) == 0 {
		return 0, nil
	}

	var query strings.Builder
	// A source without an EUR quote must not wipe the EUR price another source
	// (or the FX fallback) already produced.
	query.WriteString(`UPDATE cards AS c
SET price_usd = v.usd,
    price_eur = CASE WHEN v.eur > 0 THEN v.eur ELSE c.price_eur END,
    price_eur_estimated = CASE WHEN v.eur > 0 THEN FALSE ELSE c.price_eur_estimated END,
    last_updated = NOW()
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
  AND (c.price_usd IS DISTINCT FROM v.usd
       OR (v.eur > 0 AND c.price_eur IS DISTINCT FROM v.eur))`)

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
