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
	"log/slog"
	"pokget/internal/models"
	"pokget/internal/service"
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
	}
}

func (w *DataSyncWorker) Start(ctx context.Context) {
	slog.Info("Data Sync Worker starting", "interval", w.interval)
	priceTicker := time.NewTicker(w.interval)
	metadataTicker := time.NewTicker(24 * time.Hour) // Sync metadata daily
	repairTicker := time.NewTicker(1 * time.Hour)    // Check for missing fingerprints hourly
	defer priceTicker.Stop()
	defer metadataTicker.Stop()
	defer repairTicker.Stop()

	// Initial sync
	if w.metadataClient != nil {
		go w.syncMetadata(ctx)
	}
	if w.metadataService != nil {
		go w.syncMissingFingerprints(ctx)
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
			w.syncPrices()
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

	rows, err := w.db.Query("SELECT id, name, image_url, game, language FROM cards WHERE phash IS NULL LIMIT 100")
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

		_, err = w.db.Exec("UPDATE cards SET phash = $1 WHERE id = $2", processed.Phash, processed.ID)
		if err != nil {
			slog.Error("Repair: Failed to update card fingerprint", "id", c.ID, "error", err)
		} else {
			slog.Info("Repair: Generated missing fingerprint", "id", c.ID, "name", c.Name)
		}

		// Rate limit downloads during repair to be nice to APIs
		time.Sleep(500 * time.Millisecond)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Repair: Row iteration error", "error", err)
	}
	slog.Info("Missing fingerprints repair cycle completed")
	if w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

func (w *DataSyncWorker) Stop() {
	close(w.stop)
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
		_, err := w.db.Exec(`
			INSERT INTO cards (id, name, set_name, image_url, game, price_usd, price_eur)
			VALUES ($1, $2, $3, $4, $5, 0, 0)
			ON CONFLICT (id) DO NOTHING`,
			c.ID, c.Name, c.Set, c.ImageURL, c.Game)
		if err != nil {
			slog.Warn("Failed to upsert card", "card_id", c.ID, "error", err)
		}

		// New card found! Process and insert fingerprint
		func() {
			limiter := time.NewTicker(500 * time.Millisecond)
			defer limiter.Stop()
			<-limiter.C

			processed, err := w.metadataService.ProcessCard(ctx, c)
			if err != nil {
				slog.Error("Sync: Failed to process card", "id", c.ID, "error", err)
				return
			}

			_, err = w.db.Exec(`
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
	if w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

func (w *DataSyncWorker) syncPrices() {
	slog.Info("Starting price synchronization cycle")
	rows, err := w.db.Query("SELECT id, name, set_name, price_usd, price_eur FROM cards")
	if err != nil {
		slog.Error("Sync: Failed to query cards", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var c models.Card
		if err := rows.Scan(&c.ID, &c.Name, &c.Set, &c.PriceUSD, &c.PriceEUR); err != nil {
			slog.Error("Sync: Failed to scan card", "error", err)
			continue
		}

		usd, eur, err := w.priceClient.FetchPrice(c)
		if err != nil {
			slog.Error("Sync: Failed to fetch price", "card", c.Name, "error", err)
			continue
		}

		// Guard against a failed/empty scrape returning (0, 0): writing those
		// would wipe a valid stored price and pollute price history with zeros.
		if usd == 0 && eur == 0 {
			slog.Warn("Sync: Skipping card with zero price (likely failed scrape)", "card", c.Name)
			continue
		}

		// 1. Update Card Price and Record Price History in a transaction
		tx, err := w.db.Begin()
		if err != nil {
			slog.Error("Failed to begin transaction for price update", "card", c.Name, "error", err)
			continue
		}
		_, err = tx.Exec("UPDATE cards SET price_usd = $1, price_eur = $2, last_updated = NOW() WHERE id = $3",
			decimal.NewFromFloat(usd), decimal.NewFromFloat(eur), c.ID)
		if err != nil {
			tx.Rollback()
			slog.Error("Failed to update card price", "card", c.Name, "error", err)
			continue
		}
		_, err = tx.Exec("INSERT INTO price_history (card_id, price_usd, price_eur) VALUES ($1, $2, $3)",
			c.ID, decimal.NewFromFloat(usd), decimal.NewFromFloat(eur))
		if err != nil {
			tx.Rollback()
			slog.Error("Failed to insert price history", "card", c.Name, "error", err)
			continue
		}
		if err := tx.Commit(); err != nil {
			slog.Error("Failed to commit price update transaction", "card", c.Name, "error", err)
		} else {
			slog.Debug("Sync: Updated card price", "card", c.Name, "usd", usd, "eur", eur)
		}

		// 3. Check Price Alerts (Improvement #38)
		w.checkPriceAlerts(c, usd)
	}
	slog.Info("Price synchronization cycle completed")
	if w.OnSyncComplete != nil {
		w.OnSyncComplete()
	}
}

// checkPriceAlerts evaluates active price alerts for a card against its current
// USD price. It is a dedicated method so the result set is closed when this call
// returns rather than accumulating open cursors for the duration of syncPrices
// (a `defer` inside the per-card loop would leak connections across all cards).
func (w *DataSyncWorker) checkPriceAlerts(c models.Card, usd float64) {
	rowsAlerts, err := w.db.Query("SELECT id, user_id, target_price FROM price_alerts WHERE card_id = $1 AND is_active = TRUE", c.ID)
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
}
