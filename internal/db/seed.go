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

package db

import (
	"database/sql"
	"pokget/internal/models"
	"log/slog"

	"github.com/shopspring/decimal"
)

func SeedDatabase(db *sql.DB) error {
	slog.Info("Worker: Seeding database with initial card data...")

	// Safety check: verify migration 16 applied the game column
	var exists bool
	err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='cards' AND column_name='game')").Scan(&exists)
	if err != nil || !exists {
		slog.Warn("Skipping database seeding: 'game' column not found yet. Migration 16 might be pending.")
		return nil
	}

	mockCards := []models.Card{
		{ID: "swsh45-19", Name: "Charizard VMAX", Set: "Shining Fates", PriceUSD: decimal.NewFromFloat(120.50), PriceEUR: decimal.NewFromFloat(110.00), ImageURL: "https://images.pokemontcg.io/swsh45/19_hires.png", Variant: "Holo", Game: "Pokemon"},
		{ID: "swsh7-215", Name: "Umbreon VMAX", Set: "Evolving Skies", PriceUSD: decimal.NewFromFloat(650.00), PriceEUR: decimal.NewFromFloat(600.00), ImageURL: "https://images.pokemontcg.io/swsh7/215_hires.png", Variant: "Alt Art", Game: "Pokemon"},
		{ID: "op01-016", Name: "Nami", Set: "Romance Dawn", PriceUSD: decimal.NewFromFloat(15.00), PriceEUR: decimal.NewFromFloat(14.00), ImageURL: "https://example.com/op01-016.png", Variant: "Parallel", Game: "One Piece"},
		{ID: "op01-120", Name: "Shanks", Set: "Romance Dawn", PriceUSD: decimal.NewFromFloat(250.00), PriceEUR: decimal.NewFromFloat(230.00), ImageURL: "https://example.com/op01-120.png", Variant: "SEC", Game: "One Piece"},
	}

	for _, card := range mockCards {
		_, err := db.Exec(`
			INSERT INTO cards (id, name, set_name, image_url, price_usd, price_eur, variant, game)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO NOTHING`,
			card.ID, card.Name, card.Set, card.ImageURL, card.PriceUSD, card.PriceEUR, card.Variant, card.Game)
		if err != nil {
			return err
		}
	}

	return nil
}
