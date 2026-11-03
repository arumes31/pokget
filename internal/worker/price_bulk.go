package worker

import (
	"context"
	"errors"
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

const (
	// fxBatchSize bounds one converted-EUR statement so it cannot hold locks on
	// the whole catalog while the per-card sync runs.
	fxBatchSize = 20000
	// fxBatchLimit stops a runaway loop when the batches never drain.
	fxBatchLimit = 200
	// fxRetryLimit is how often a conflicting transaction is retried per batch.
	fxRetryLimit = 3
)

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
	// Rarities ride along with the same payload: several catalog sources (TCGdex
	// lists sets without a rarity field) leave the column empty.
	fuzzyRarity := make(map[fuzzyScope]map[string]string)
	// Old TCGs (World of Warcraft, Sorcery) list their products without a collector
	// number, so those can only be matched by the card name inside the set.
	fuzzyByNameNumberless := make(map[fuzzyScope]map[string]float64)
	// Composite numbers carry the set code themselves; Weiss Schwarz abbreviates the
	// set as "NIK/" while the number says "NIK/S135-E063".
	fuzzyByNumber := make(map[fuzzyScope]map[string]float64)
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

	rememberRarity := func(scope fuzzyScope, set, number, rarity string) {
		keys := priceMatchKeys(set, number)
		if len(keys) == 0 {
			return
		}
		index := fuzzyRarity[scope]
		if index == nil {
			index = make(map[string]string)
			fuzzyRarity[scope] = index
		}
		for _, key := range keys {
			if _, exists := index[key]; !exists {
				index[key] = rarity
			}
		}
	}

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		changed, err := retryOnConflict(ctx, fxRetryLimit, func() (int64, error) {
			return w.applyBulkBatch(ctx, batch)
		})
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
			scope := newFuzzyScope(update.Game, update.Languages)
			for _, bound := range update.Languages {
				if boundLanguages[update.Game] == nil {
					boundLanguages[update.Game] = make(map[string]struct{})
				}
				boundLanguages[update.Game][bound] = struct{}{}
			}
			if update.SetCode != "" {
				rememberKeys(fuzzy, scope, update.SetCode, update.Number, update.USD)
			}
			for _, alias := range setNameAliases(update.SetName) {
				rememberKeys(fuzzyByName, scope, alias, update.Number, update.USD)
			}
			if key := priceNumberOnlyKey(update.Number); key != "" {
				if fuzzyByNumber[scope] == nil {
					fuzzyByNumber[scope] = make(map[string]float64)
				}
				if _, exists := fuzzyByNumber[scope][key]; !exists {
					fuzzyByNumber[scope][key] = update.USD
				}
			}
		}
		if update.Game != "" && update.Number == "" && update.ProductName != "" && update.SetName != "" && update.USD > 0 {
			scope := newFuzzyScope(update.Game, update.Languages)
			if fuzzyByNameNumberless[scope] == nil {
				fuzzyByNameNumberless[scope] = make(map[string]float64)
			}
			for _, alias := range setNameAliases(update.SetName) {
				key := priceNameKey(alias, update.ProductName)
				if key == "" {
					continue
				}
				if _, exists := fuzzyByNameNumberless[scope][key]; !exists {
					fuzzyByNameNumberless[scope][key] = update.USD
				}
			}
		}
		if update.Game != "" && update.Number != "" && update.Rarity != "" {
			scope := newFuzzyScope(update.Game, update.Languages)
			if update.SetCode != "" {
				rememberRarity(scope, update.SetCode, update.Number, update.Rarity)
			}
			for _, alias := range setNameAliases(update.SetName) {
				rememberRarity(scope, alias, update.Number, update.Rarity)
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
	// The fallback keeps every normalized key in memory until the batch is done.
	// Log the footprint so a growing catalog can be watched instead of guessed.
	slog.Debug("Bulk prices: fallback index built", "source", source.Name(),
		"set_keys", countIndexKeys(fuzzy), "name_keys", countIndexKeys(fuzzyByName),
		"numberless_keys", countIndexKeys(fuzzyByNameNumberless), "number_keys", countIndexKeys(fuzzyByNumber))
	// A category whose products all lack a collector number (World of Warcraft)
	// only fills the name index, so the loop walks the union of both indexes.
	scopes := make(map[fuzzyScope]struct{}, len(fuzzy)+len(fuzzyByNameNumberless)+len(fuzzyByNumber))
	for scope := range fuzzy {
		scopes[scope] = struct{}{}
	}
	for scope := range fuzzyByNameNumberless {
		scopes[scope] = struct{}{}
	}
	for scope := range fuzzyByNumber {
		scopes[scope] = struct{}{}
	}
	// Language-bound scopes run first: the Japanese Pokémon prices must win over
	// the language-agnostic pass that would otherwise settle for an English one.
	for _, scope := range orderedFuzzyScopes(scopes) {
		if err := ctx.Err(); err != nil {
			return err
		}
		var exclude []string
		if scope.languages == "" {
			for language := range boundLanguages[scope.game] {
				exclude = append(exclude, language)
			}
			sort.Strings(exclude)
		}
		changed, err := w.applyFuzzyPricesExcluding(ctx, scope.game, scope.languageList(), exclude,
			fuzzy[scope], fuzzyByName[scope], fuzzyByNameNumberless[scope], fuzzyByNumber[scope])
		if err != nil {
			slog.Warn("Bulk prices: fallback match failed", "source", source.Name(), "game", scope.game, "languages", scope.languages, "error", err)
			continue
		}
		fuzzyUpdated += changed
	}

	var rarityUpdated int64
	for _, scope := range orderedFuzzyScopes(fuzzyRarity) {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A language-bound pass must not exclude its own languages.
		var bound map[string]struct{}
		if scope.languages == "" {
			bound = boundLanguages[scope.game]
		}
		changed, err := w.applyFuzzyRarity(ctx, scope.game, scope.languageList(), bound, fuzzyRarity[scope])
		if err != nil {
			slog.Warn("Bulk prices: rarity match failed", "source", source.Name(), "game", scope.game, "languages", scope.languages, "error", err)
			continue
		}
		rarityUpdated += changed
	}

	slog.Info("Bulk prices applied", "source", source.Name(), "rows", rows, "updated", updated, "fallback_updated", fuzzyUpdated, "rarity_updated", rarityUpdated)
	// The counters above count changed rows, not matched ones: a feed whose prices
	// already match the database reports zero every run. Say that instead of
	// blaming the mapping.
	if rows > 0 && updated+fuzzyUpdated+rarityUpdated == 0 {
		slog.Info("Bulk prices: source changed nothing",
			"source", source.Name(), "rows", rows,
			"hint", "every price was already stored; if that is unexpected, check the set abbreviations of this source")
	}
	return nil
}

// applyFuzzyRarity fills cards.rarity for cards whose catalog source published no
// rarity. The marketplace rarity rides along with the price payload, so this costs
// no extra request.
func (w *DataSyncWorker) applyFuzzyRarity(ctx context.Context, game string, languages []string, bound map[string]struct{}, rarities map[string]string) (int64, error) {
	if len(rarities) == 0 {
		return 0, nil
	}
	excluded := make([]string, 0, len(bound))
	for excludedLanguage := range bound {
		excluded = append(excluded, excludedLanguage)
	}
	sort.Strings(excluded)

	const query = `
		SELECT id, COALESCE(set_code, ''), COALESCE(set_name, ''), COALESCE(collector_number, '')
		FROM cards
		WHERE game = $1
		  AND superseded_by_card_id IS NULL
		  AND (rarity IS NULL OR rarity = '')
		  AND collector_number IS NOT NULL AND collector_number <> ''
		  AND ($2::text[] = '{}' OR language = ANY($2::text[]))
		  AND COALESCE(language, '') <> ALL($3::text[])`
	rows, err := w.db.QueryContext(ctx, query, game, pq.Array(nonNil(languages)), pq.Array(excluded))
	if err != nil {
		return 0, fmt.Errorf("rarity query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	lookupKeys := func(index map[string]string, keys []string) (string, bool) {
		for _, key := range keys {
			if rarity, ok := index[key]; ok {
				return rarity, true
			}
		}
		return "", false
	}
	lookup := func(index map[string]string, set, number string) (string, bool) {
		return lookupKeys(index, priceMatchKeys(set, number))
	}

	updates := make(map[string]string)
	for rows.Next() {
		var id, setCode, setName, number string
		if err := rows.Scan(&id, &setCode, &setName, &number); err != nil {
			continue
		}
		if setCode != "" {
			if rarity, ok := lookup(rarities, setCode, number); ok {
				updates[id] = rarity
				continue
			}
		}
		if setName != "" {
			if rarity, ok := lookup(rarities, setName, number); ok {
				updates[id] = rarity
				continue
			}
		}
		// The rarity rides along with the price payload, so a set the marketplace
		// only knows under an alias must resolve here as well.
		if codeKeys, nameKeys := setAliasKeys(game, setCode, number); len(codeKeys)+len(nameKeys) > 0 {
			if rarity, ok := lookupKeys(rarities, codeKeys); ok {
				updates[id] = rarity
				continue
			}
			if rarity, ok := lookupKeys(rarities, nameKeys); ok {
				updates[id] = rarity
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	ids := make([]string, 0, len(updates))
	for id := range updates {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var total int64
	batch := make([]service.BulkPriceUpdate, 0, bulkPriceBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		changed, err := retryOnConflict(ctx, fxRetryLimit, func() (int64, error) {
			return w.applyRarityByCardID(ctx, batch)
		})
		if err != nil {
			return err
		}
		total += changed
		batch = batch[:0]
		return nil
	}
	for _, id := range ids {
		batch = append(batch, service.BulkPriceUpdate{SourceCardID: id, Rarity: updates[id]})
		if len(batch) >= bulkPriceBatchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

// applyRarityByCardID writes rarities for explicit card ids that still have none.
func (w *DataSyncWorker) applyRarityByCardID(ctx context.Context, batch []service.BulkPriceUpdate) (int64, error) {
	if len(batch) == 0 {
		return 0, nil
	}
	var query strings.Builder
	query.WriteString(`UPDATE cards AS c
SET rarity = v.rarity
FROM (VALUES `)
	args := make([]any, 0, len(batch)*2)
	for index, update := range batch {
		if index > 0 {
			query.WriteString(", ")
		}
		base := index * 2
		fmt.Fprintf(&query, "($%d, $%d)", base+1, base+2)
		args = append(args, update.SourceCardID, update.Rarity)
	}
	query.WriteString(`) AS v(id, rarity)
WHERE c.id = v.id AND (c.rarity IS NULL OR c.rarity = '')`)
	result, err := w.db.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("rarity update: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
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
	// Convert in id-ordered slices. A single statement would lock hundreds of
	// thousands of rows for seconds and deadlock with the per-card price sync,
	// which updates single rows in its own transaction.
	var total int64
	for batch := 0; batch < fxBatchLimit; batch++ {
		if err := ctx.Err(); err != nil {
			break
		}
		changed, err := w.applyEURBatchWithRetry(ctx, rate)
		if err != nil {
			slog.Warn("Price FX: conversion failed", "error", err, "converted", total)
			break
		}
		total += changed
		// Rows the sync already priced are skipped by the row recheck, so a short
		// batch does not mean the catalog is done; only an empty batch does.
		if changed == 0 {
			break
		}
	}
	if total > 0 {
		slog.Info("Price FX: derived EUR prices from USD", "rate", rate, "rows", total)
	}
}

// applyEURBatchWithRetry retries a batch that lost a lock race against the
// per-card price sync instead of giving up on the whole conversion.
func (w *DataSyncWorker) applyEURBatchWithRetry(ctx context.Context, rate float64) (int64, error) {
	return retryOnConflict(ctx, fxRetryLimit, func() (int64, error) {
		return w.applyEURBatch(ctx, rate, fxBatchSize)
	})
}

// retryOnConflict reruns a statement that lost a lock race with another writer
// (the per-card price sync or the catalog sync). Without the retry a single
// deadlock throws away a whole batch of already matched prices.
func retryOnConflict(ctx context.Context, attempts int, run func() (int64, error)) (int64, error) {
	var lastErr error
	for attempt := 0; attempt <= attempts; attempt++ {
		changed, err := run()
		if err == nil {
			return changed, nil
		}
		if !retryablePostgresError(err) {
			return 0, err
		}
		lastErr = err
		if !waitForContext(ctx, time.Duration(attempt+1)*time.Second) {
			return 0, ctx.Err()
		}
	}
	return 0, lastErr
}

// applyEURBatch converts one id-ordered slice of unpriced EUR values.
func (w *DataSyncWorker) applyEURBatch(ctx context.Context, rate float64, size int) (int64, error) {
	// The eligibility check is repeated on the target row: the per-card sync can
	// write a native EUR quote between the subquery and the row lock, and that
	// fresh quote must survive. PostgreSQL re-evaluates the outer predicates on the
	// locked row version, the subquery alone would overwrite it.
	result, err := w.db.ExecContext(ctx, `
		UPDATE cards
		SET price_eur = ROUND(price_usd * $1, 2), price_eur_estimated = TRUE
		WHERE id IN (
			SELECT id FROM cards
			WHERE superseded_by_card_id IS NULL
			  AND price_usd > 0
			  AND (price_eur IS NULL OR price_eur <= 0)
			ORDER BY id
			LIMIT $2
		)
		  AND superseded_by_card_id IS NULL
		  AND price_usd > 0
		  AND (price_eur IS NULL OR price_eur <= 0)`, rate, size)
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return changed, nil
}

// retryablePostgresError reports the transaction conflicts a retry can resolve.
func retryablePostgresError(err error) bool {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) {
		return false
	}
	// 40P01 deadlock_detected, 40001 serialization_failure, 55P03 lock_not_available
	return pgErr.Code == "40P01" || pgErr.Code == "40001" || pgErr.Code == "55P03"
}

// fuzzyScope limits a fallback match to one game and, for marketplaces that
// split a game by language, to those languages. The language list is stored as a
// sorted, comma separated string so the scope stays usable as a map key.
type fuzzyScope struct {
	game      string
	languages string
}

// newFuzzyScope builds the scope for a marketplace row. An empty language list
// means "every language of the game".
func newFuzzyScope(game string, languages []string) fuzzyScope {
	if len(languages) == 0 {
		return fuzzyScope{game: game}
	}
	sorted := make([]string, 0, len(languages))
	for _, language := range languages {
		language = strings.ToLower(strings.TrimSpace(language))
		if language != "" {
			sorted = append(sorted, language)
		}
	}
	sort.Strings(sorted)
	return fuzzyScope{game: game, languages: strings.Join(sorted, ",")}
}

// languageList returns the scope's languages, or nil for "any language".
func (s fuzzyScope) languageList() []string {
	if s.languages == "" {
		return nil
	}
	return strings.Split(s.languages, ",")
}

// countIndexKeys sums the keys of a per-scope index.
func countIndexKeys[V any](index map[fuzzyScope]map[string]V) int {
	total := 0
	for _, entries := range index {
		total += len(entries)
	}
	return total
}

// orderedFuzzyScopes returns the scopes of an index in a deterministic order with
// the language-bound scopes first.
func orderedFuzzyScopes[V any](index map[fuzzyScope]V) []fuzzyScope {
	scopes := make([]fuzzyScope, 0, len(index))
	for scope := range index {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if (scopes[i].languages == "") != (scopes[j].languages == "") {
			return scopes[i].languages != ""
		}
		if scopes[i].game != scopes[j].game {
			return scopes[i].game < scopes[j].game
		}
		return scopes[i].languages < scopes[j].languages
	})
	return scopes
}

// applyFuzzyPrices fills prices for cards that have none yet by matching the
// normalized set code and collector number, then the normalized set name and
// number. Existing prices are never replaced, so dedicated sources (Scryfall,
// YGOPRODeck) keep their values.
func (w *DataSyncWorker) applyFuzzyPrices(ctx context.Context, game string, languages []string, prices, namePrices map[string]float64) (int64, error) {
	return w.applyFuzzyPricesExcluding(ctx, game, languages, nil, prices, namePrices, nil, nil)
}

// applyFuzzyPricesExcluding is applyFuzzyPrices with a list of languages the
// match must skip. The generic marketplace category of a game uses it to leave
// the languages alone that have their own category.
//
// nameOnlyPrices keys the card name inside the set and covers marketplaces that
// list old cards without a collector number (World of Warcraft, Sorcery).
func (w *DataSyncWorker) applyFuzzyPricesExcluding(
	ctx context.Context,
	game string,
	languages []string,
	excludeLanguages []string,
	prices, namePrices map[string]float64,
	nameOnlyPrices, numberOnlyPrices map[string]float64,
) (int64, error) {
	if len(prices) == 0 && len(namePrices) == 0 && len(nameOnlyPrices) == 0 && len(numberOnlyPrices) == 0 {
		return 0, nil
	}
	// One static statement keeps every value a bound parameter: $2 restricts the
	// match to the given languages (empty = all), $3 lists the languages to skip.
	const query = `
		SELECT id, COALESCE(set_code, ''), COALESCE(set_name, ''), COALESCE(collector_number, ''), COALESCE(name, '')
		FROM cards
		WHERE game = $1
		  AND superseded_by_card_id IS NULL
		  AND (price_usd IS NULL OR price_usd <= 0)
		  AND ($2::text[] = '{}' OR language = ANY($2::text[]))
		  AND COALESCE(language, '') <> ALL($3::text[])`
	// pq.Array(nil) encodes SQL NULL, and `<> ALL(NULL)` is NULL for every row, so
	// an empty list has to be an empty array.
	rows, err := w.db.QueryContext(ctx, query, game, pq.Array(nonNil(languages)), pq.Array(nonNil(excludeLanguages)))
	if err != nil {
		return 0, fmt.Errorf("fallback price query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	lookupKeys := func(index map[string]float64, keys []string) (float64, bool) {
		for _, key := range keys {
			if price, ok := index[key]; ok {
				return price, true
			}
		}
		return 0, false
	}
	lookup := func(index map[string]float64, set, number string) (float64, bool) {
		return lookupKeys(index, priceMatchKeys(set, number))
	}

	updates := make(map[string]float64)
	for rows.Next() {
		var id, setCode, setName, number, name string
		if err := rows.Scan(&id, &setCode, &setName, &number, &name); err != nil {
			continue
		}
		if number != "" {
			if setCode != "" {
				if price, ok := lookup(prices, setCode, number); ok {
					updates[id] = price
					continue
				}
			}
			if setName != "" && len(namePrices) > 0 {
				if price, ok := lookup(namePrices, setName, number); ok {
					updates[id] = price
					continue
				}
			}
			// Games whose catalog set codes never appear in the marketplace use a
			// configured mapping instead of the normalized set key.
			if codeKeys, nameKeys := setAliasKeys(game, setCode, number); len(codeKeys)+len(nameKeys) > 0 {
				if price, ok := lookupKeys(prices, codeKeys); ok {
					updates[id] = price
					continue
				}
				if price, ok := lookupKeys(namePrices, nameKeys); ok {
					updates[id] = price
					continue
				}
			}
		}
		// A composite number that carries the set code matches even when the
		// marketplace abbreviates the set differently.
		if len(numberOnlyPrices) > 0 && number != "" {
			if key := priceNumberOnlyKey(number); key != "" {
				if price, ok := numberOnlyPrices[key]; ok {
					updates[id] = price
					continue
				}
			}
		}
		// Products without a collector number can only be matched by name.
		if len(nameOnlyPrices) > 0 && name != "" && setName != "" {
			if key := priceNameKey(setName, name); key != "" {
				if price, ok := nameOnlyPrices[key]; ok {
					updates[id] = price
				}
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
		changed, err := retryOnConflict(ctx, fxRetryLimit, func() (int64, error) {
			return w.applyPriceByCardID(ctx, batch)
		})
		if err != nil {
			return err
		}
		total += changed
		pending = pending[:0]
		return nil
	}
	// Sorted ids give every batch the same lock order as the next one, which keeps
	// the fallback from deadlocking with the per-card price sync.
	ids := make([]string, 0, len(updates))
	for id := range updates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		pending = append(pending, id)
		values[id] = updates[id]
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
	// A source without a quote for one currency must not wipe the price another
	// source (or the FX fallback) already produced: Scryfall emits foil-only and
	// EUR-only printings with USD 0.
	query.WriteString(`UPDATE cards AS c
SET price_usd = CASE WHEN v.usd > 0 THEN v.usd ELSE c.price_usd END,
    price_eur = CASE WHEN v.eur > 0 THEN v.eur ELSE c.price_eur END,
    price_eur_estimated = CASE WHEN v.eur > 0 THEN FALSE ELSE c.price_eur_estimated END,
    price_variant = CASE WHEN v.usd > 0 AND v.variant <> '' THEN v.variant ELSE c.price_variant END,
    -- A variant-only update must not claim the price changed.
    last_updated = CASE WHEN (v.usd > 0 AND c.price_usd IS DISTINCT FROM v.usd)
                          OR (v.eur > 0 AND c.price_eur IS DISTINCT FROM v.eur)
                        THEN NOW() ELSE c.last_updated END
FROM (VALUES `)
	args := make([]any, 0, len(batch)*5)
	for index, update := range batch {
		if index > 0 {
			query.WriteString(", ")
		}
		base := index * 5
		fmt.Fprintf(&query, "($%d, $%d, $%d::numeric, $%d::numeric, $%d)", base+1, base+2, base+3, base+4, base+5)
		args = append(args, update.SourceID, update.SourceCardID, update.USD, update.EUR, update.Variant)
	}
	query.WriteString(`) AS v(source_id, source_card_id, usd, eur, variant)
WHERE c.source_id = v.source_id
  AND c.source_card_id = v.source_card_id
  AND ((v.usd > 0 AND c.price_usd IS DISTINCT FROM v.usd)
       OR (v.eur > 0 AND c.price_eur IS DISTINCT FROM v.eur)
       OR (v.usd > 0 AND v.variant <> '' AND c.price_variant IS DISTINCT FROM v.variant))`)

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
