package models

import "github.com/shopspring/decimal"

type Card struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Set       string          `json:"set"`
	PriceUSD  decimal.Decimal `json:"price_usd"`
	PriceEUR  decimal.Decimal `json:"price_eur"`
	ImageURL  string          `json:"image_url"`
	Change24h float64         `json:"change_24h"`
	Variant   string          `json:"variant"` // Holo, Reverse Holo, etc.
	Language  string          `json:"language"` // en, jp, de, etc.
	Phash     *int64          `json:"phash"`    // Perceptual hash for visual matching
}
