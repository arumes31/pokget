package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"
)

// APICardPrice resolves the current market price for a card on demand. The
// scanner calls it right after identifying a card so the price is visible
// immediately instead of waiting for the next price-sync cycle.
func (h *Handler) APICardPrice(w http.ResponseWriter, r *http.Request) {
	cardID := strings.TrimSpace(r.URL.Query().Get("id"))
	if cardID == "" {
		http.Error(w, "Missing card id", http.StatusBadRequest)
		return
	}
	if h.DB == nil {
		http.Error(w, "Price lookup unavailable", http.StatusServiceUnavailable)
		return
	}

	card, err := loadPricedCard(r, h.DB, cardID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Card not found", http.StatusNotFound)
			return
		}
		slog.Warn("Price lookup: failed to load card", "card_id", cardID, "error", err)
		http.Error(w, "Price lookup failed", http.StatusInternalServerError)
		return
	}

	storedEUR, _ := card.PriceEUR.Float64()
	storedUSD, _ := card.PriceUSD.Float64()

	if h.Prices == nil {
		// Without a resolver the stored catalog price is still useful.
		h.writePriceResponse(w, r, service.PriceQuote{USD: storedUSD, EUR: storedEUR})
		return
	}

	quote, err := h.Prices.Resolve(r.Context(), card)
	if err != nil {
		slog.Info("Price lookup: no market price", "card_id", cardID, "card", card.Name, "error", err)
		quote = service.PriceQuote{USD: storedUSD, EUR: storedEUR}
	} else {
		h.persistResolvedPrice(r, card.ID, quote)
	}

	h.writePriceResponse(w, r, quote)
}

// persistResolvedPrice writes freshly resolved prices back to the catalog so
// the next scan (and the portfolio) can use them.
func (h *Handler) persistResolvedPrice(r *http.Request, cardID string, quote service.PriceQuote) {
	if h.DB == nil || (quote.USD <= 0 && quote.EUR <= 0) {
		return
	}
	_, err := h.DB.ExecContext(r.Context(), `
		UPDATE cards
		SET price_usd = CASE WHEN $1 > 0 THEN $1 ELSE price_usd END,
		    price_eur = CASE WHEN $2 > 0 THEN $2 ELSE price_eur END,
		    last_updated = NOW()
		WHERE id = $3`, quote.USD, quote.EUR, cardID)
	if err != nil {
		slog.Warn("Price lookup: failed to persist resolved price", "card_id", cardID, "error", err)
	}
}

func (h *Handler) writePriceResponse(w http.ResponseWriter, r *http.Request, quote service.PriceQuote) {
	currency := h.userCurrency(r)
	price := quote.EUR
	if currency == "USD" {
		price = quote.USD
	}
	if price <= 0 {
		// Fall back to whichever market has data so the scanner always shows a value.
		if quote.EUR > 0 {
			price, currency = quote.EUR, "EUR"
		} else if quote.USD > 0 {
			price, currency = quote.USD, "USD"
		}
	}
	payload := map[string]any{
		"id":          strings.TrimSpace(r.URL.Query().Get("id")),
		"price":       price,
		"currency":    currency,
		"usd":         quote.USD,
		"eur":         quote.EUR,
		"cached":      quote.Cached,
		"unavailable": price <= 0,
	}
	if quote.Source != "" {
		payload["source"] = quote.Source
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("Price lookup: failed to encode response", "error", err)
	}
}

func (h *Handler) userCurrency(r *http.Request) string {
	currency := "EUR"
	if userID, ok := r.Context().Value(auth.UserContextKey{}).(string); ok && h.DB != nil {
		_ = h.DB.QueryRowContext(r.Context(), "SELECT currency FROM users WHERE id = $1", userID).Scan(&currency)
	}
	return currency
}

func loadPricedCard(r *http.Request, db *sql.DB, cardID string) (models.Card, error) {
	var card models.Card
	err := db.QueryRowContext(r.Context(), `
		SELECT id, name, COALESCE(set_name, ''), COALESCE(set_code, ''),
		       COALESCE(collector_number, ''), COALESCE(game, ''), COALESCE(language, ''),
		       COALESCE(rarity, ''), COALESCE(variant, ''),
		       COALESCE(source_id, ''), COALESCE(source_card_id, ''),
		       COALESCE(price_usd, 0), COALESCE(price_eur, 0)
		FROM cards
		WHERE id = $1 AND superseded_by_card_id IS NULL`, cardID).
		Scan(&card.ID, &card.Name, &card.Set, &card.SetCode, &card.CollectorNumber, &card.Game,
			&card.Language, &card.Rarity, &card.Variant, &card.SourceID, &card.SourceCardID,
			&card.PriceUSD, &card.PriceEUR)
	if err != nil {
		return models.Card{}, err
	}
	card.Language = strings.TrimSpace(card.Language)
	return card, nil
}
