// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"pokget/internal/models"
	"pokget/internal/service"
	"slices"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type DataSyncWorker struct {
	db              *sql.DB
	priceClient     service.PriceClient
	priceClients    map[string][]service.PriceClient
	metadataClient  service.MetadataClient
	metadataService *service.MetadataService
	interval        time.Duration
	metadataTargets []MetadataTarget
	limiter         interface{ Wait(context.Context) error }
	retryAttempts   int
	retryBaseDelay  time.Duration
	circuitFailures int
	circuitCooldown time.Duration
	maxPriceRatio   float64
	failureSink     FailureSink
	lease           CycleLease
	circuitMu       sync.Mutex
	circuits        map[string]providerCircuit
	repairMu        sync.Mutex
	repairAfterID   string
	stop            chan struct{}
	done            chan struct{}
	stopOnce        sync.Once
	tasks           sync.WaitGroup
	OnSyncComplete  func()
}

func NewDataSyncWorker(db *sql.DB, pc service.PriceClient, mc service.MetadataClient, ms *service.MetadataService, interval time.Duration) *DataSyncWorker {
	config := defaultDataSyncConfig(interval)
	worker, err := NewConfiguredDataSyncWorker(db, pc, mc, ms, config)
	if err != nil {
		panic(err)
	}
	return worker
}

// NewConfiguredDataSyncWorker constructs a worker with explicit provider,
// retry, anomaly, and coordination policies.
func NewConfiguredDataSyncWorker(
	db *sql.DB,
	pc service.PriceClient,
	mc service.MetadataClient,
	ms *service.MetadataService,
	config DataSyncConfig,
) (*DataSyncWorker, error) {
	if db == nil {
		return nil, errors.New("worker: database is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}

	priceClients := make(map[string][]service.PriceClient, len(config.PriceClients))
	for game, clients := range config.PriceClients {
		priceClients[models.NormalizeGame(game)] = slices.Clone(clients)
	}
	return &DataSyncWorker{
		db:              db,
		priceClient:     pc,
		priceClients:    priceClients,
		metadataClient:  mc,
		metadataService: ms,
		interval:        config.Interval,
		metadataTargets: slices.Clone(config.MetadataTargets),
		limiter:         newRequestLimiter(config.RequestsPerSecond, config.RequestBurst),
		retryAttempts:   config.RetryAttempts,
		retryBaseDelay:  config.RetryBaseDelay,
		circuitFailures: config.CircuitFailures,
		circuitCooldown: config.CircuitCooldown,
		maxPriceRatio:   config.MaxPriceRatio,
		failureSink:     config.FailureSink,
		lease:           config.Lease,
		circuits:        make(map[string]providerCircuit),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}, nil
}

func (w *DataSyncWorker) Start(ctx context.Context) {
	defer func() {
		w.tasks.Wait()
		close(w.done)
	}()
	slog.Info("Data Sync Worker starting", "interval", w.interval)
	priceTicker := time.NewTicker(w.interval)
	metadataTicker := time.NewTicker(24 * time.Hour) // Sync metadata daily
	repairTicker := time.NewTicker(1 * time.Hour)    // Check for missing fingerprints hourly
	defer priceTicker.Stop()
	defer metadataTicker.Stop()
	defer repairTicker.Stop()

	// Initial sync
	if w.metadataClient != nil {
		w.tasks.Add(1)
		go func() {
			defer w.tasks.Done()
			w.syncMetadata(ctx)
		}()
	}
	if w.metadataService != nil {
		w.tasks.Add(1)
		go func() {
			defer w.tasks.Done()
			w.syncMissingFingerprints(ctx)
		}()
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Data Sync Worker stopping (context cancelled)")
			return
		case <-w.stop:
			slog.Info("Data Sync Worker stopping (stop signal)")
			return
		case <-priceTicker.C:
			w.syncPrices(ctx)
		case <-metadataTicker.C:
			if w.metadataClient != nil {
				w.syncMetadata(ctx)
			}
		case <-repairTicker.C:
			if w.metadataService != nil {
				w.syncMissingFingerprints(ctx)
			}
		}
	}
}

func (w *DataSyncWorker) syncMissingFingerprints(ctx context.Context) {
	w.repairMu.Lock()
	defer w.repairMu.Unlock()

	release, acquired, err := w.acquireCycle(ctx, "fingerprints")
	if err != nil {
		slog.Error("Repair: Failed to acquire cycle lease", "error", err)
		return
	}
	if !acquired {
		slog.Debug("Repair: Another replica owns the cycle")
		return
	}
	defer w.releaseCycle(release, "fingerprints")

	slog.Info("Starting missing fingerprints repair cycle")

	rows, err := w.db.QueryContext(ctx, `
		SELECT id, name, image_url, game, language
		FROM cards
		WHERE phash IS NULL
		  AND superseded_by_card_id IS NULL
		  AND id > $1
		ORDER BY id
		LIMIT 100`, w.repairAfterID)
	if err != nil {
		slog.Error("Repair: Failed to query cards missing fingerprints", "error", err)
		return
	}

	cards := make([]models.Card, 0, 100)
	for rows.Next() {
		var c models.Card
		if err := rows.Scan(&c.ID, &c.Name, &c.ImageURL, &c.Game, &c.Language); err != nil {
			slog.Warn("Repair: Failed to scan card", "error", err)
			continue
		}
		cards = append(cards, c)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil {
		slog.Error("Repair: Row iteration error", "error", rowErr)
		return
	}
	if closeErr != nil {
		slog.Warn("Repair: Failed to close rows", "error", closeErr)
	}
	if len(cards) == 0 {
		w.repairAfterID = ""
		return
	}

	for _, c := range cards {
		if ctx.Err() != nil {
			slog.Info("Repair: Stopping due to context cancellation")
			break
		}
		w.repairAfterID = c.ID

		processed, err := w.metadataService.ProcessCard(ctx, c)
		if err != nil {
			slog.Error("Repair: Failed to process card", "id", c.ID, "error", err)
			continue
		}

		_, err = w.db.ExecContext(ctx, "UPDATE cards SET phash = $1 WHERE id = $2", processed.Phash, processed.ID)
		if err != nil {
			slog.Error("Repair: Failed to update card fingerprint", "id", c.ID, "error", err)
		} else {
			slog.Info("Repair: Generated missing fingerprint", "id", c.ID, "name", c.Name)
		}

		// Rate limit downloads during repair while remaining responsive to
		// shutdown cancellation.
		if !waitForContext(ctx, 500*time.Millisecond) {
			break
		}
	}
	slog.Info("Missing fingerprints repair cycle completed")
	if ctx.Err() == nil && w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

func (w *DataSyncWorker) Stop() {
	w.stopOnce.Do(func() { close(w.stop) })
}

func (w *DataSyncWorker) Wait(ctx context.Context) error {
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases resources owned by configured provider clients. Call it only
// after Start has returned or Wait has completed.
func (w *DataSyncWorker) Close() error {
	clients := []service.PriceClient{w.priceClient}
	for _, configured := range w.priceClients {
		clients = append(clients, configured...)
	}
	seen := make(map[string]struct{}, len(clients))
	var closeErrors []error
	for _, client := range clients {
		closer, ok := client.(interface{ Close() error })
		if !ok || client == nil {
			continue
		}
		key := fmt.Sprintf("%T:%p", client, client)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if err := closer.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

func (w *DataSyncWorker) syncMetadata(ctx context.Context) {
	release, acquired, err := w.acquireCycle(ctx, "metadata")
	if err != nil {
		slog.Error("Sync: Failed to acquire metadata lease", "error", err)
		return
	}
	if !acquired {
		slog.Debug("Sync: Another replica owns metadata synchronization")
		return
	}
	defer w.releaseCycle(release, "metadata")

	slog.Info("Starting metadata synchronization cycle")

	if w.metadataService == nil {
		slog.Error("Sync: metadataService is nil, skipping cycle")
		return
	}

	targets := w.metadataTargets
	if len(targets) == 0 {
		targets = []MetadataTarget{{Game: "pokemon", Language: "en"}}
	}

	for _, target := range targets {
		cards, fetchErr := w.metadataClient.FetchCards(ctx, target.Game, target.Language)
		if fetchErr != nil {
			slog.Error("Sync: Failed to fetch cards", "game", target.Game, "language", target.Language, "error", fetchErr)
			w.storeFailure(ctx, FailureRecord{
				OccurredAt: time.Now().UTC(),
				Operation:  "metadata",
				Game:       models.NormalizeGame(target.Game),
				Attempts:   1,
				Error:      fetchErr.Error(),
			})
			continue
		}
		w.syncMetadataCards(ctx, cards)
	}
	slog.Info("Metadata synchronization cycle completed")
	if ctx.Err() == nil && w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

func (w *DataSyncWorker) syncMetadataCards(ctx context.Context, cards []models.Card) {
	for _, c := range cards {
		// Check for cancellation before processing each card
		if ctx.Err() != nil {
			slog.Info("Sync: Stopping metadata sync due to context cancellation")
			break
		}

		// Use INSERT...ON CONFLICT DO NOTHING to eliminate N+1 SELECT EXISTS pattern
		_, err := w.db.ExecContext(ctx, `
			INSERT INTO cards (id, name, set_name, image_url, game, price_usd, price_eur)
			VALUES ($1, $2, $3, $4, $5, 0, 0)
			ON CONFLICT (id) DO NOTHING`,
			c.ID, c.Name, c.Set, c.ImageURL, c.Game)
		if err != nil {
			slog.Warn("Failed to upsert card", "card_id", c.ID, "error", err)
		}

		// New card found! Process and insert fingerprint
		func() {
			if !waitForContext(ctx, 500*time.Millisecond) {
				return
			}

			processed, err := w.metadataService.ProcessCard(ctx, c)
			if err != nil {
				slog.Error("Sync: Failed to process card", "id", c.ID, "error", err)
				return
			}

			_, err = w.db.ExecContext(ctx, `
				UPDATE cards SET phash = $1, image_url = $2 WHERE id = $3 AND phash IS NULL`,
				processed.Phash, processed.ImageURL, processed.ID)

			if err != nil {
				slog.Error("Sync: Failed to update card fingerprint", "id", c.ID, "error", err)
			} else {
				slog.Info("Sync: Added new card with fingerprint", "id", c.ID, "name", c.Name)
			}
		}()
	}
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (w *DataSyncWorker) acquireCycle(
	ctx context.Context,
	name string,
) (func() error, bool, error) {
	if w.lease == nil {
		return func() error { return nil }, true, nil
	}
	return w.lease.Acquire(ctx, "pokget:data-sync:"+name)
}

func (w *DataSyncWorker) releaseCycle(release func() error, name string) {
	if release == nil {
		return
	}
	if err := release(); err != nil {
		slog.Error("Sync: Failed to release cycle lease", "cycle", name, "error", err)
	}
}

func (w *DataSyncWorker) storeFailure(ctx context.Context, failure FailureRecord) {
	if w.failureSink == nil {
		return
	}
	if err := w.failureSink.StoreFailure(ctx, failure); err != nil {
		slog.Error("Sync: Failed to persist worker failure", "operation", failure.Operation, "error", err)
	}
}

func (w *DataSyncWorker) syncPrices(ctx context.Context) {
	release, acquired, err := w.acquireCycle(ctx, "prices")
	if err != nil {
		slog.Error("Sync: Failed to acquire price lease", "error", err)
		return
	}
	if !acquired {
		slog.Debug("Sync: Another replica owns price synchronization")
		return
	}
	defer w.releaseCycle(release, "prices")

	slog.Info("Starting price synchronization cycle")
	if w.priceClient == nil && len(w.priceClients) == 0 {
		slog.Error("Sync: price client is nil, skipping cycle")
		return
	}

	// A full catalog can contain tens of thousands of printings. Scraping every
	// catalog row would take longer than the sync interval and unnecessarily hit
	// third-party sites. Refresh only cards a user is actively tracking, oldest
	// first, in a bounded batch.
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, name, set_name, COALESCE(price_usd, 0), COALESCE(price_eur, 0), COALESCE(game, '')
		FROM cards
		WHERE superseded_by_card_id IS NULL
		  AND id IN (
			SELECT card_id FROM portfolio
			UNION
			SELECT card_id FROM wantlist
			UNION
			SELECT card_id FROM price_alerts WHERE is_active = TRUE
		  )
		ORDER BY last_updated ASC NULLS FIRST,
		         GREATEST(COALESCE(price_usd, 0), COALESCE(price_eur, 0)) DESC,
		         id
		LIMIT 100`)
	if err != nil {
		slog.Error("Sync: Failed to query cards", "error", err)
		return
	}
	defer rows.Close()
	updated := false

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			slog.Info("Sync: Stopping price cycle due to context cancellation", "error", err)
			return
		}

		var c models.Card
		if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR, &c.Game); err != nil {
			slog.Error("Sync: Failed to scan card", "error", err)
			continue
		}

		usd, eur, err := w.fetchCardPrice(ctx, c)
		if err != nil {
			slog.Error("Sync: Failed to fetch price", "card", c.Name, "error", err)
			w.storeFailure(ctx, cardFailure("price", c, w.retryAttempts, err))
			continue
		}

		validUSD := usd > 0 && !math.IsNaN(usd) && !math.IsInf(usd, 0)
		validEUR := eur > 0 && !math.IsNaN(eur) && !math.IsInf(eur, 0)
		if validUSD && w.priceAnomalous(c.PriceUSD, usd) {
			slog.Warn("Sync: Rejected anomalous USD price", "card", c.Name, "current", c.PriceUSD, "candidate", usd)
			validUSD = false
		}
		if validEUR && w.priceAnomalous(c.PriceEUR, eur) {
			slog.Warn("Sync: Rejected anomalous EUR price", "card", c.Name, "current", c.PriceEUR, "candidate", eur)
			validEUR = false
		}
		// A source can fail independently. Preserve its existing currency value
		// instead of replacing it with zero or a non-finite number.
		if !validUSD && !validEUR {
			slog.Warn("Sync: Skipping card with zero price (likely failed scrape)", "card", c.Name)
			continue
		}
		nextUSD := c.PriceUSD
		if validUSD {
			nextUSD = decimal.NewFromFloat(usd)
		}
		nextEUR := c.PriceEUR
		if validEUR {
			nextEUR = decimal.NewFromFloat(eur)
		}

		// 1. Update Card Price and Record Price History in a transaction
		tx, err := w.db.BeginTx(ctx, nil)
		if err != nil {
			slog.Error("Failed to begin transaction for price update", "card", c.Name, "error", err)
			continue
		}
		_, err = tx.ExecContext(ctx, "UPDATE cards SET price_usd = $1, price_eur = $2, last_updated = NOW() WHERE id = $3",
			nextUSD, nextEUR, c.ID)
		if err != nil {
			tx.Rollback()
			slog.Error("Failed to update card price", "card", c.Name, "error", err)
			continue
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO price_history (card_id, price_usd, price_eur) VALUES ($1, $2, $3)",
			c.ID, nextUSD, nextEUR)
		if err != nil {
			tx.Rollback()
			slog.Error("Failed to insert price history", "card", c.Name, "error", err)
			continue
		}
		if err := tx.Commit(); err != nil {
			slog.Error("Failed to commit price update transaction", "card", c.Name, "error", err)
			continue
		}
		slog.Debug("Sync: Updated card price", "card", c.Name, "usd", usd, "eur", eur)
		updated = true

		// 3. Check Price Alerts (Improvement #38)
		if validUSD {
			w.checkPriceAlerts(ctx, c, usd)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("Sync: Failed while reading cards", "error", err)
	}
	slog.Info("Price synchronization cycle completed")
	if updated && w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

func fetchPrice(ctx context.Context, client service.PriceClient, card models.Card) (float64, float64, error) {
	if contextClient, ok := client.(service.ContextPriceClient); ok {
		return contextClient.FetchPriceContext(ctx, card)
	}
	return client.FetchPrice(card)
}

func (w *DataSyncWorker) fetchCardPrice(
	ctx context.Context,
	card models.Card,
) (float64, float64, error) {
	clients := w.priceClients[models.NormalizeGame(card.Game)]
	if len(clients) == 0 {
		clients = w.priceClients["*"]
	}
	if len(clients) == 0 && w.priceClient != nil {
		clients = []service.PriceClient{w.priceClient}
	}
	if len(clients) == 0 {
		return 0, 0, errors.New("no price source configured for game")
	}

	usdValues := make([]float64, 0, len(clients))
	eurValues := make([]float64, 0, len(clients))
	errs := make([]error, 0, len(clients))
	for index, client := range clients {
		key := fmt.Sprintf("%s:%T:%d", models.NormalizeGame(card.Game), client, index)
		usd, eur, err := w.fetchSourceWithRetry(ctx, key, client, card)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			continue
		}
		if usd > 0 && !math.IsNaN(usd) && !math.IsInf(usd, 0) {
			usdValues = append(usdValues, usd)
		}
		if eur > 0 && !math.IsNaN(eur) && !math.IsInf(eur, 0) {
			eurValues = append(eurValues, eur)
		}
	}
	if len(usdValues) == 0 && len(eurValues) == 0 && len(errs) > 0 {
		return 0, 0, errors.Join(errs...)
	}
	return median(usdValues), median(eurValues), nil
}

func (w *DataSyncWorker) fetchSourceWithRetry(
	ctx context.Context,
	key string,
	client service.PriceClient,
	card models.Card,
) (float64, float64, error) {
	if client == nil {
		return 0, 0, errors.New("nil price source")
	}
	if !w.circuitReady(key, time.Now()) {
		return 0, 0, ErrWorkerCircuitOpen
	}

	var lastErr error
	for attempt := 0; attempt < w.retryAttempts; attempt++ {
		if err := w.limiter.Wait(ctx); err != nil {
			return 0, 0, err
		}
		usd, eur, err := fetchPrice(ctx, client, card)
		if err == nil {
			w.circuitSucceeded(key)
			return usd, eur, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, 0, err
		}
		if errors.Is(err, service.ErrPriceSourceBlocked) {
			w.circuitBlocked(key, time.Now())
			return 0, 0, err
		}
		if attempt+1 < w.retryAttempts {
			if err := waitForRetry(ctx, retryDelay(w.retryBaseDelay, attempt)); err != nil {
				return 0, 0, err
			}
		}
	}
	w.circuitFailed(key, time.Now())
	return 0, 0, lastErr
}

func (w *DataSyncWorker) circuitReady(key string, now time.Time) bool {
	w.circuitMu.Lock()
	defer w.circuitMu.Unlock()
	circuit := w.circuits[key]
	if circuit.openUntil.After(now) {
		return false
	}
	if !circuit.openUntil.IsZero() {
		delete(w.circuits, key)
	}
	return true
}

func (w *DataSyncWorker) circuitSucceeded(key string) {
	w.circuitMu.Lock()
	delete(w.circuits, key)
	w.circuitMu.Unlock()
}

func (w *DataSyncWorker) circuitFailed(key string, now time.Time) {
	w.circuitMu.Lock()
	defer w.circuitMu.Unlock()
	circuit := w.circuits[key]
	circuit.failures++
	if circuit.failures >= w.circuitFailures {
		circuit.openUntil = now.Add(w.circuitCooldown)
	}
	w.circuits[key] = circuit
}

func (w *DataSyncWorker) circuitBlocked(key string, now time.Time) {
	w.circuitMu.Lock()
	w.circuits[key] = providerCircuit{
		failures:  w.circuitFailures,
		openUntil: now.Add(w.circuitCooldown),
	}
	w.circuitMu.Unlock()
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func (w *DataSyncWorker) priceAnomalous(current decimal.Decimal, candidate float64) bool {
	if w.maxPriceRatio <= 1 || !current.IsPositive() || candidate <= 0 {
		return false
	}
	currentValue, _ := current.Float64()
	ratio := candidate / currentValue
	return ratio > w.maxPriceRatio || ratio < 1/w.maxPriceRatio
}

// checkPriceAlerts evaluates active price alerts for a card against its current
// USD price. It is a dedicated method so the result set is closed when this call
// returns rather than accumulating open cursors for the duration of syncPrices
// (a `defer` inside the per-card loop would leak connections across all cards).
func (w *DataSyncWorker) checkPriceAlerts(ctx context.Context, c models.Card, usd float64) {
	rowsAlerts, err := w.db.QueryContext(ctx, "SELECT id, user_id, target_price FROM price_alerts WHERE card_id = $1 AND is_active = TRUE", c.ID)
	if err != nil {
		slog.Warn("Failed to query price alerts", "card_id", c.ID, "error", err)
		return
	}
	type priceAlert struct {
		id          int
		userID      string
		targetPrice decimal.Decimal
	}
	alerts := make([]priceAlert, 0)
	currentPrice := decimal.NewFromFloat(usd)
	for rowsAlerts.Next() {
		var alert priceAlert
		if err := rowsAlerts.Scan(&alert.id, &alert.userID, &alert.targetPrice); err != nil {
			slog.Warn("Failed to scan price alert row", "error", err)
			continue
		}
		alerts = append(alerts, alert)
	}
	rowErr := rowsAlerts.Err()
	closeErr := rowsAlerts.Close()
	if rowErr != nil {
		slog.Warn("Failed while reading price alerts", "card_id", c.ID, "error", rowErr)
		return
	}
	if closeErr != nil {
		slog.Warn("Failed to close price alert rows", "card_id", c.ID, "error", closeErr)
	}

	for _, alert := range alerts {
		if !currentPrice.LessThanOrEqual(alert.targetPrice) {
			continue
		}
		result, err := w.db.ExecContext(ctx, `
			UPDATE price_alerts
			SET is_active = FALSE
			WHERE id = $1 AND is_active = TRUE`, alert.id)
		if err != nil {
			slog.Warn("Failed to claim triggered price alert", "alert_id", alert.id, "error", err)
			continue
		}
		claimed, err := result.RowsAffected()
		if err != nil || claimed != 1 {
			continue
		}
		slog.Info("ALERT: Price target hit!", "user", alert.userID, "card", c.Name, "target", alert.targetPrice, "current", currentPrice)
		// Delivery can now be retried independently without sending this alert
		// twice from overlapping worker replicas.
	}
}
