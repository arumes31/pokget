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
	"pokget/internal/auth"
	"pokget/internal/models"
)

func (h *Handler) Wantlist(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Wantlist", "method", r.Method, "url", r.URL.String())
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := h.DB.Query(`
		SELECT w.id, w.card_id, w.target_price, w.notes, c.name, c.set_name, c.price_usd, c.image_url
		FROM wantlist w
		JOIN cards c ON w.card_id = c.id
		WHERE w.user_id = $1`, userID)
	if err != nil {
		slog.Error("Failed to fetch wantlist", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []models.WantlistItem
	for rows.Next() {
		var i models.WantlistItem
		if err := rows.Scan(&i.ID, &i.CardID, &i.TargetPrice, &i.Notes, &i.Card.Name, &i.Card.Set, &i.Card.PriceUSD, &i.Card.ImageURL); err == nil {
			items = append(items, i)
		}
	}

	h.render(w, r, "wantlist.html", map[string]interface{}{
		"Items": items,
	})
}

func (h *Handler) AddToWantlist(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: AddToWantlist", "method", r.Method, "url", r.URL.String())
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cardID := r.FormValue("card_id")
	if cardID == "" {
		http.Error(w, "card_id is required", http.StatusBadRequest)
		return
	}
	targetPrice := r.FormValue("target_price")
	notes := r.FormValue("notes")

	_, err := h.DB.Exec(`
		INSERT INTO wantlist (user_id, card_id, target_price, notes)
		VALUES ($1, $2, $3, $4)`,
		userID, cardID, targetPrice, notes)
	if err != nil {
		slog.Error("Failed to add to wantlist", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", `{"notify": {"msg": "Identify Success: Grail added to Hunt", "type": "success"}}`)
	
	// Re-fetch and render the updated wantlist
	h.Wantlist(w, r)
}
