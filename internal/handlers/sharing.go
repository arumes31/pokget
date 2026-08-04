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

package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"pokget/internal/auth"
	"pokget/internal/models"
	"strings"

	"github.com/gorilla/mux"
)

func (h *Handler) PublicVault(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: PublicVault", "method", r.Method)
	vars := mux.Vars(r)
	slug := vars["slug"]

	var userID string
	var email string
	var rank string
	var xp int
	var currency string

	err := h.DB.QueryRowContext(r.Context(), `
		SELECT id, email, COALESCE(rank_title, ''), COALESCE(xp, 0), COALESCE(currency, 'EUR')
		FROM users 
		WHERE public_slug = $1 AND is_public_profile = TRUE`,
		slug).Scan(&userID, &email, &rank, &xp, &currency)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Warn("PublicVault: Vault not found", "slug", slug)
			http.Error(w, "Vault not found", http.StatusNotFound)
		} else {
			slog.Error("PublicVault: Database error", "slug", slug, "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fetch public portfolio items
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT p.id, p.condition, p.format, p.grade, p.grading_company, p.notes, 
		       c.name, c.set_name, COALESCE(c.price_usd, 0), COALESCE(c.price_eur, 0),
		       COALESCE(c.image_url, ''), COALESCE(c.game, '')
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.user_id = $1 AND p.is_public = TRUE`, userID)

	if err != nil {
		slog.Error("Failed to fetch public vault", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	portfolio := make([]models.PortfolioItem, 0, 64) // BOLT OPTIMIZATION: Pre-allocate slice to reduce memory allocations
	for rows.Next() {
		var p models.PortfolioItem
		var grade, gradingCompany, notes sql.NullString
		if err := rows.Scan(&p.ID, &p.Condition, &p.Format, &grade, &gradingCompany, &notes,
			&p.Card.Name, &p.Card.Set, &p.Card.PriceUSD, &p.Card.PriceEUR, &p.Card.ImageURL, &p.Card.Game); err != nil {
			slog.Error("Failed to scan public vault item", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		p.Grade = grade.String
		p.GradingCompany = gradingCompany.String
		p.Notes = notes.String
		portfolio = append(portfolio, p)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed while reading public vault", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	// BOLT OPTIMIZATION: Use strings.IndexByte for more efficient string extraction (one less allocation than Split)
	username := email
	if idx := strings.IndexByte(email, '@'); idx != -1 {
		username = email[:idx]
	}
	currencySymbol := "€"
	if currency == "USD" {
		currencySymbol = "$"
	}

	h.render(w, r, "public_vault.html", map[string]interface{}{
		"Username":       username,
		"Portfolio":      portfolio,
		"Rank":           rank,
		"XP":             xp,
		"IsPublic":       true,
		"UserCurrency":   currency,
		"CurrencySymbol": currencySymbol,
	})
}

func (h *Handler) ToggleVisibility(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ToggleVisibility", "method", r.Method, "url", r.URL.String())
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	itemID := r.FormValue("item_id")
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}
	isPublic := r.FormValue("is_public") == "true"

	result, err := h.DB.ExecContext(r.Context(), "UPDATE portfolio SET is_public = $1 WHERE id = $2 AND user_id = $3", isPublic, itemID, userID)
	if err != nil {
		http.Error(w, "Failed to update visibility", http.StatusInternalServerError)
		return
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		http.Error(w, "Portfolio item not found", http.StatusNotFound)
		return
	}

	// BUG-M09 FIX: Set Content-Type header for API responses.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}
