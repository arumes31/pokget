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
	
	err := h.DB.QueryRow(`
		SELECT id, email, rank_title, xp 
		FROM users 
		WHERE public_slug = $1 AND is_public_profile = TRUE`, 
		slug).Scan(&userID, &email, &rank, &xp)
	
	if err != nil {
		if err == sql.ErrNoRows {
			slog.Warn("PublicVault: Vault not found", "slug", slug)
			http.Error(w, "Vault not found", http.StatusNotFound)
		} else {
			slog.Error("PublicVault: Database error", "slug", slug, "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Fetch public portfolio items
	rows, err := h.DB.Query(`
		SELECT p.id, p.condition, p.format, p.grade, p.grading_company, p.notes, 
		       c.name, c.set_name, c.price_usd, c.image_url, c.game
		FROM portfolio p
		JOIN cards c ON p.card_id = c.id
		WHERE p.user_id = $1 AND p.is_public = TRUE`, userID)
	
	if err != nil {
		slog.Error("Failed to fetch public vault", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var portfolio []models.PortfolioItem
	for rows.Next() {
		var p models.PortfolioItem
		if err := rows.Scan(&p.ID, &p.Condition, &p.Format, &p.Grade, &p.GradingCompany, &p.Notes,
			&p.Card.Name, &p.Card.Set, &p.Card.PriceUSD, &p.Card.ImageURL, &p.Card.Game); err == nil {
			portfolio = append(portfolio, p)
		}
	}



	// Performance Optimization (Bolt): Using IndexByte + slicing instead of strings.Split
	// to avoid allocating a slice of strings.
	// Expected Impact: Reduces memory allocations per request by 1.
	username := email
	if i := strings.IndexByte(email, '@'); i != -1 {
		username = email[:i]
	}

	h.render(w, r, "public_vault.html", map[string]interface{}{
		"Username":  username,
		"Portfolio": portfolio,
		"Rank":      rank,
		"XP":        xp,
		"IsPublic":  true,
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
	isPublic := r.FormValue("is_public") == "true"

	_, err := h.DB.Exec("UPDATE portfolio SET is_public = $1 WHERE id = $2 AND user_id = $3", isPublic, itemID, userID)
	if err != nil {
		http.Error(w, "Failed to update visibility", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
