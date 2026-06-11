// Copyright (c) 2026 arumes31
package service

import (
	"context"
	"database/sql"
	"log/slog"
	"pokget/internal/models"
	"time"
)

type WorkerService struct {
	DB          *sql.DB
	PriceClient PriceClient
}

func NewWorkerService(db *sql.DB, priceClient PriceClient) *WorkerService {
	return &WorkerService{
		DB:          db,
		PriceClient: priceClient,
	}
}

// StartBackgroundPriceScraper runs every 24 hours to refresh all card prices
func (s *WorkerService) StartBackgroundPriceScraper(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	slog.Info("Worker: Background Price Scraper started")

	// Run once immediately on start
	s.refreshAllPrices()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Worker: Background Price Scraper stopping")
			return
		case <-ticker.C:
			s.refreshAllPrices()
		}
	}
}

func (s *WorkerService) refreshAllPrices() {
	slog.Info("Worker: Refreshing all card prices...")

	rows, err := s.DB.Query("SELECT id, name, set_name, game FROM cards")
	if err != nil {
		slog.Error("Worker: Failed to fetch cards for refresh", "error", err)
		return
	}

	var cards []models.Card
	for rows.Next() {
		var card models.Card
		if err := rows.Scan(&card.ID, &card.Name, &card.Set, &card.Game); err != nil {
			slog.Error("Worker: Failed to scan card row", "error", err)
			continue
		}
		cards = append(cards, card)
	}
	// Early release of database connection
	if err := rows.Close(); err != nil {
		slog.Error("Failed to close rows", "error", err)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for _, card := range cards {
		usd, eur, err := s.PriceClient.FetchPrice(card)
		if err != nil {
			slog.Warn("Worker: Failed to fetch price for card", "card", card.Name, "error", err)
			<-ticker.C
			continue
		}

		_, err = s.DB.Exec(`
			UPDATE cards 
			SET price_usd = $1, price_eur = $2, last_updated = CURRENT_TIMESTAMP 
			WHERE id = $3`,
			usd, eur, card.ID)

		if err != nil {
			slog.Error("Worker: Failed to update card price", "card", card.Name, "error", err)
		} else {
			slog.Info("Worker: Updated card price", "card", card.Name, "usd", usd, "eur", eur)
		}

		// Use ticker to decouple fetching from DB updates and network delay
		<-ticker.C
	}
}
