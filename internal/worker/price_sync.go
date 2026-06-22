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
	"pokget/internal/models"
	"pokget/internal/service"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"
)

type SyncedCard struct {
	card models.Card
	usd  float64
}


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


	var syncedCards []SyncedCard

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

		// 2. Record Price History (Improvement #26)
		_, err = w.db.Exec("INSERT INTO price_history (card_id, price_usd, price_eur) VALUES ($1, $2, $3)",
			c.ID, decimal.NewFromFloat(usd), decimal.NewFromFloat(eur))
		if err != nil {
			slog.Error("Sync: Failed to record price history", "card", c.Name, "error", err)
		}

		// 3. Check Price Alerts (Improvement #38)
		syncedCards = append(syncedCards, SyncedCard{card: c, usd: usd})
	}

	// 3. Check Price Alerts (Improvement #38)
	w.checkPriceAlerts(syncedCards)
	slog.Info("Price synchronization cycle completed")
}

// checkPriceAlerts evaluates active price alerts batched against all updated prices.
func (w *PriceSyncWorker) checkPriceAlerts(synced []SyncedCard) {
	if len(synced) == 0 {
		return
	}

	rowsAlerts, err := w.db.Query("SELECT id, user_id, target_price, card_id FROM price_alerts WHERE is_active = TRUE")
	if err != nil {
		return
	}
	defer rowsAlerts.Close()

	// Map card_id to synced card data for fast lookup
	prices := make(map[string]SyncedCard, len(synced))
	for _, sc := range synced {
		prices[sc.card.ID] = sc
	}

	for rowsAlerts.Next() {
		var alertID int
		var userID string
		var targetPrice decimal.Decimal
		var cardID string
		if err := rowsAlerts.Scan(&alertID, &userID, &targetPrice, &cardID); err != nil {
			continue
		}

		sc, exists := prices[cardID]
		if !exists {
			continue
		}

		currentPrice := decimal.NewFromFloat(sc.usd)
		if currentPrice.LessThanOrEqual(targetPrice) {
			slog.Info("ALERT: Price target hit!", "user", userID, "card", sc.card.Name, "target", targetPrice, "current", currentPrice)
			// In a real app, send email/push here.
		}
	}
}
