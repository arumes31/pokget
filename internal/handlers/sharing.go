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
	"log/slog"
	"net/http"
	"pokget/internal/db"
	"pokget/internal/models"
	"github.com/gorilla/mux"
)

func (h *Handler) PublicVault(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: PublicVault", "method", r.Method, "url", r.URL.String())
	vars := mux.Vars(r)
	userID := vars["user_id"]

	if userID == "" {
		http.Error(w, "User ID required", http.StatusBadRequest)
		return
	}

	// Fetch public portfolio items
	rows, err := db.DB.Query(`
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

	// Fetch user info (rank, xp)
	var rank string
	var xp int
	_ = db.DB.QueryRow("SELECT rank_title, xp FROM users WHERE id = $1", userID).Scan(&rank, &xp)

	h.render(w, r, "public_vault.html", map[string]interface{}{
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

	userID := r.Context().Value("user_id").(string)
	itemID := r.FormValue("item_id")
	isPublic := r.FormValue("is_public") == "true"

	_, err := db.DB.Exec("UPDATE portfolio SET is_public = $1 WHERE id = $2 AND user_id = $3", isPublic, itemID, userID)
	if err != nil {
		http.Error(w, "Failed to update visibility", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
