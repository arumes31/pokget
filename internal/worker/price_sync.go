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
	"log/slog"
	"math"
	"pokget/internal/models"
	"pokget/internal/service"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type DataSyncWorker struct {
	db              *sql.DB
	priceClient     service.PriceClient
	metadataClient  service.MetadataClient
	metadataService *service.MetadataService
	interval        time.Duration
	stop            chan struct{}
	done            chan struct{}
	stopOnce        sync.Once
	tasks           sync.WaitGroup
	OnSyncComplete  func()
}

func NewDataSyncWorker(db *sql.DB, pc service.PriceClient, mc service.MetadataClient, ms *service.MetadataService, interval time.Duration) *DataSyncWorker {
	return &DataSyncWorker{
		db:              db,
		priceClient:     pc,
		metadataClient:  mc,
		metadataService: ms,
		interval:        interval,
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
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
	slog.Info("Starting missing fingerprints repair cycle")

	rows, err := w.db.QueryContext(ctx, "SELECT id, name, image_url, game, language FROM cards WHERE phash IS NULL AND superseded_by_card_id IS NULL LIMIT 100")
	if err != nil {
		slog.Error("Repair: Failed to query cards missing fingerprints", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		// Check for cancellation before processing each card
		if ctx.Err() != nil {
			slog.Info("Repair: Stopping due to context cancellation")
			break
		}

		var c models.Card
		if err := rows.Scan(&c.ID, &c.Name, &c.ImageURL, &c.Game, &c.Language); err != nil {
			continue
		}

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
	if err := rows.Err(); err != nil {
		slog.Error("Repair: Row iteration error", "error", err)
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

func (w *DataSyncWorker) syncMetadata(ctx context.Context) {
	slog.Info("Starting metadata synchronization cycle")

	if w.metadataService == nil {
		slog.Error("Sync: metadataService is nil, skipping cycle")
		return
	}

	// Support Pokemon/English for POC
	cards, err := w.metadataClient.FetchCards(ctx, "Pokemon", "en")
	if err != nil {
		slog.Error("Sync: Failed to fetch cards", "error", err)
		return
	}

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
	slog.Info("Metadata synchronization cycle completed")
	if ctx.Err() == nil && w.OnSyncComplete != nil {
		w.OnSyncComplete()
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

func (w *DataSyncWorker) syncPrices(ctx context.Context) {
	slog.Info("Starting price synchronization cycle")
	if w.priceClient == nil {
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
		ORDER BY last_updated ASC NULLS FIRST
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

		usd, eur, err := fetchPrice(ctx, w.priceClient, c)
		if err != nil {
			slog.Error("Sync: Failed to fetch price", "card", c.Name, "error", err)
			if errors.Is(err, service.ErrPriceSourceBlocked) {
				slog.Warn("Sync: Price source blocked; ending cycle early")
				break
			}
			continue
		}

		validUSD := usd > 0 && !math.IsNaN(usd) && !math.IsInf(usd, 0)
		validEUR := eur > 0 && !math.IsNaN(eur) && !math.IsInf(eur, 0)
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
	defer rowsAlerts.Close()

	currentPrice := decimal.NewFromFloat(usd)
	for rowsAlerts.Next() {
		var alertID int
		var userID string
		var targetPrice decimal.Decimal
		if err := rowsAlerts.Scan(&alertID, &userID, &targetPrice); err != nil {
			slog.Warn("Failed to scan price alert row", "error", err)
			continue
		}
		if currentPrice.LessThanOrEqual(targetPrice) {
			slog.Info("ALERT: Price target hit!", "user", userID, "card", c.Name, "target", targetPrice, "current", currentPrice)
			// In a real app, send email/push here.
		}
	}
	if err := rowsAlerts.Err(); err != nil {
		slog.Warn("Failed while reading price alerts", "card_id", c.ID, "error", err)
	}
}
