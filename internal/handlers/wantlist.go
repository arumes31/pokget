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

type wantlistViewItem struct {
	models.WantlistItem
	PriceUSD    float64
	PriceEUR    float64
	ProgressUSD float64
	ProgressEUR float64
}

func (h *Handler) Wantlist(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Wantlist", "method", r.Method, "url", r.URL.String())
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT w.id, w.card_id, COALESCE(w.target_price, 0), COALESCE(w.notes, ''), c.name, c.set_name,
		       COALESCE(c.price_usd, 0), COALESCE(c.price_eur, 0), COALESCE(c.image_url, '')
		FROM wantlist w
		JOIN cards c ON w.card_id = c.id
		WHERE w.user_id = $1`, userID)
	if err != nil {
		slog.Error("Failed to fetch wantlist", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]wantlistViewItem, 0, 64)
	for rows.Next() {
		var item wantlistViewItem
		if err := rows.Scan(&item.ID, &item.CardID, &item.TargetPrice, &item.Notes, &item.Card.Name, &item.Card.Set, &item.Card.PriceUSD, &item.Card.PriceEUR, &item.Card.ImageURL); err != nil {
			slog.Error("Failed to scan wantlist item", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		item.PriceUSD, _ = item.Card.PriceUSD.Float64()
		item.PriceEUR, _ = item.Card.PriceEUR.Float64()
		if item.TargetPrice > 0 {
			item.ProgressUSD = item.PriceUSD / item.TargetPrice * 100
			item.ProgressEUR = item.PriceEUR / item.TargetPrice * 100
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed while reading wantlist", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
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
	var cardExists bool
	if err := h.DB.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM cards WHERE id = $1)", cardID).Scan(&cardExists); err != nil {
		slog.Error("Failed to validate wantlist card", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !cardExists {
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}

	// BUG-H10 FIX: Parse target_price as float64 to match the SQL DECIMAL type.
	// Previously, the raw string was passed directly to SQL, which could cause
	// conversion errors or type mismatches with the DECIMAL(12,2) column.
	targetPriceStr := r.FormValue("target_price")
	targetPrice, err := parseOptionalPrice(targetPriceStr)
	if err != nil {
		http.Error(w, "Invalid target price", http.StatusBadRequest)
		return
	}

	notes := r.FormValue("notes")

	_, err = h.DB.ExecContext(r.Context(), `
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

func (h *Handler) UpdateWantlistItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	itemID := r.FormValue("item_id")
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}
	targetPrice, err := parseFiniteFloat(r.FormValue("target_price"), 0, maxStoredPrice)
	if err != nil {
		http.Error(w, "Invalid target price", http.StatusBadRequest)
		return
	}
	result, err := h.DB.ExecContext(r.Context(), `
		UPDATE wantlist SET target_price = $1, notes = $2
		WHERE id = $3 AND user_id = $4`, targetPrice, r.FormValue("notes"), itemID, userID)
	if err != nil {
		slog.Error("Failed to update wantlist item", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		http.Error(w, "Wantlist item not found", http.StatusNotFound)
		return
	}
	w.Header().Set("HX-Trigger", `{"notify": {"msg": "Wantlist item updated", "type": "success"}}`)
	h.Wantlist(w, r)
}

func (h *Handler) DeleteWantlistItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	itemID := r.FormValue("item_id")
	if itemID == "" {
		http.Error(w, "item_id is required", http.StatusBadRequest)
		return
	}
	result, err := h.DB.ExecContext(r.Context(), "DELETE FROM wantlist WHERE id = $1 AND user_id = $2", itemID, userID)
	if err != nil {
		slog.Error("Failed to delete wantlist item", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		http.Error(w, "Wantlist item not found", http.StatusNotFound)
		return
	}
	w.Header().Set("HX-Trigger", `{"notify": {"msg": "Wantlist item removed", "type": "success"}}`)
	h.Wantlist(w, r)
}
