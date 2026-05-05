package db

import (
	"database/sql"
	"gettos/internal/models"
	"log/slog"

	"github.com/shopspring/decimal"
)

func SeedDatabase(db *sql.DB) error {
	slog.Info("Worker: Seeding database with initial card data...")

	mockCards := []models.Card{
		{ID: "swsh45-19", Name: "Charizard VMAX", Set: "Shining Fates", PriceUSD: decimal.NewFromFloat(120.50), PriceEUR: decimal.NewFromFloat(110.00), ImageURL: "https://images.pokemontcg.io/swsh45/19_hires.png", Variant: "Holo"},
		{ID: "swsh7-215", Name: "Umbreon VMAX", Set: "Evolving Skies", PriceUSD: decimal.NewFromFloat(650.00), PriceEUR: decimal.NewFromFloat(600.00), ImageURL: "https://images.pokemontcg.io/swsh7/215_hires.png", Variant: "Alt Art"},
		{ID: "swsh12-186", Name: "Lugia V", Set: "Silver Tempest", PriceUSD: decimal.NewFromFloat(180.00), PriceEUR: decimal.NewFromFloat(165.00), ImageURL: "https://images.pokemontcg.io/swsh12/186_hires.png", Variant: "Holo"},
	}

	for _, card := range mockCards {
		_, err := db.Exec(`
			INSERT INTO cards (id, name, set_name, image_url, price_usd, price_eur, variant)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO NOTHING`,
			card.ID, card.Name, card.Set, card.ImageURL, card.PriceUSD, card.PriceEUR, card.Variant)
		if err != nil {
			return err
		}
	}

	return nil
}
