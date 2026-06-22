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
	"fmt"
	"strings"
	"database/sql"
	"pokget/internal/models"
	"pokget/internal/service"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"
)

type PriceSyncWorker struct {
	db          *sql.DB
	priceClient service.PriceClient
	interval    time.Duration
	stop        chan struct{}
}

func NewPriceSyncWorker(db *sql.DB, pc service.PriceClient, interval time.Duration) *PriceSyncWorker {
	return &PriceSyncWorker{
		db:          db,
		priceClient: pc,
		interval:    interval,
		stop:        make(chan struct{}),
	}
}

func (w *PriceSyncWorker) Start(ctx context.Context) {
	slog.Info("Price Sync Worker starting", "interval", w.interval)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Price Sync Worker stopping (context cancelled)")
			return
		case <-w.stop:
			slog.Info("Price Sync Worker stopping (stop signal)")
			return
		case <-ticker.C:
			w.syncPrices()
		}
	}
}

func (w *PriceSyncWorker) Stop() {
	close(w.stop)
}

func (w *PriceSyncWorker) syncPrices() {
	slog.Info("Starting price synchronization cycle")
	rows, err := w.db.Query("SELECT id, name, set_name, price_usd, price_eur FROM cards")
	if err != nil {
		slog.Error("Sync: Failed to query cards", "error", err)
		return
	}
	defer rows.Close()

	type historyEntry struct {
		cardID string
		usd    decimal.Decimal
		eur    decimal.Decimal
	}
	// We allocate a reasonable baseline capacity for the slice to reduce allocations.
	historyBatch := make([]historyEntry, 0, 100)

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

		// 1. Update Card Price in DB
		_, err = w.db.Exec("UPDATE cards SET price_usd = $1, price_eur = $2, last_updated = NOW() WHERE id = $3",
			decimal.NewFromFloat(usd), decimal.NewFromFloat(eur), c.ID)
		if err != nil {
			slog.Error("Sync: Failed to update DB", "card", c.Name, "error", err)
		} else {
			slog.Debug("Sync: Updated card price", "card", c.Name, "usd", usd, "eur", eur)
		}

		// 2. Queue Price History for Bulk Insert
		historyBatch = append(historyBatch, historyEntry{
			cardID: c.ID,
			usd:    decimal.NewFromFloat(usd),
			eur:    decimal.NewFromFloat(eur),
		})

		// 3. Check Price Alerts (Improvement #38)
		w.checkPriceAlerts(c, usd)
	}

	// 4. Bulk Insert Price History
	if len(historyBatch) > 0 {
		batchSize := 1000
		for i := 0; i < len(historyBatch); i += batchSize {
			end := i + batchSize
			if end > len(historyBatch) {
				end = len(historyBatch)
			}
			batch := historyBatch[i:end]

			valueStrings := make([]string, 0, len(batch))
			valueArgs := make([]interface{}, 0, len(batch)*3)

			for j, entry := range batch {
				valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d)", j*3+1, j*3+2, j*3+3))
				valueArgs = append(valueArgs, entry.cardID, entry.usd, entry.eur)
			}

			query := fmt.Sprintf("INSERT INTO price_history (card_id, price_usd, price_eur) VALUES %s", strings.Join(valueStrings, ","))
			// #nosec G201 -- valueStrings contains only hardcoded placeholders like "($1, $2, $3)" built by the logic above, no user input.
			_, err = w.db.Exec(query, valueArgs...)
			if err != nil {
				slog.Error("Sync: Failed to record price history batch", "error", err)
			}
		}
	}

	slog.Info("Price synchronization cycle completed")
}

// checkPriceAlerts evaluates active price alerts for a card against its current
// USD price. It is a dedicated method so the result set is closed when this call
// returns rather than accumulating open cursors for the duration of syncPrices
// (a `defer` inside the per-card loop would leak connections across all cards).
func (w *PriceSyncWorker) checkPriceAlerts(c models.Card, usd float64) {
	rowsAlerts, err := w.db.Query("SELECT id, user_id, target_price FROM price_alerts WHERE card_id = $1 AND is_active = TRUE", c.ID)
	if err != nil {
		return
	}
	defer rowsAlerts.Close()

	currentPrice := decimal.NewFromFloat(usd)
	for rowsAlerts.Next() {
		var alertID int
		var userID string
		var targetPrice decimal.Decimal
		if err := rowsAlerts.Scan(&alertID, &userID, &targetPrice); err != nil {
			continue
		}
		if currentPrice.LessThanOrEqual(targetPrice) {
			slog.Info("ALERT: Price target hit!", "user", userID, "card", c.Name, "target", targetPrice, "current", currentPrice)
			// In a real app, send email/push here.
		}
	}
}
