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
	"strings"
)

type ErrorCard struct {
	ID                       string
	CardID                   string
	ErrorType                string
	Description              string
	EstimatedValueMultiplier float64
	CardName                 string
	SetName                  string
	ImageURL                 string
	Game                     string
}

func (h *Handler) ErrorDatabase(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ErrorDatabase", "method", r.Method)

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT e.id, e.card_id, e.error_type, COALESCE(e.description, ''), COALESCE(e.estimated_value_multiplier, 1.0),
		       c.name, c.set_name, COALESCE(c.image_url, ''), COALESCE(c.game, '')
		FROM error_cards e
		JOIN cards c ON e.card_id = c.id
		ORDER BY e.created_at DESC`)

	if err != nil {
		slog.Error("Failed to fetch error database", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	errors := make([]ErrorCard, 0, 64) // Pre-allocate to reduce reallocations
	for rows.Next() {
		var e ErrorCard
		if err := rows.Scan(&e.ID, &e.CardID, &e.ErrorType, &e.Description, &e.EstimatedValueMultiplier, &e.CardName, &e.SetName, &e.ImageURL, &e.Game); err != nil {
			slog.Error("Failed to scan error card", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		errors = append(errors, e)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed while reading error cards", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	canSubmit := false
	if session, err := auth.Store.Get(r, "session"); err == nil {
		userID, ok := session.Values["user_id"].(string)
		canSubmit = ok && userID != ""
	}
	templateName := "error_database.html"
	if r.Header.Get("HX-Request") != "true" {
		templateName = "error_database_page.html"
	}

	h.render(w, r, templateName, map[string]interface{}{
		"Errors":    errors,
		"CanSubmit": canSubmit,
	})
}

func (h *Handler) SubmitError(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: SubmitError", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cardID := strings.TrimSpace(r.FormValue("card_id"))
	errorType := strings.TrimSpace(r.FormValue("error_type"))
	description := strings.TrimSpace(r.FormValue("description"))
	if cardID == "" || errorType == "" || description == "" {
		http.Error(w, "card_id, error_type and description are required", http.StatusBadRequest)
		return
	}
	multiplier, err := parseFiniteFloat(r.FormValue("multiplier"), 0, 100)
	if err != nil || multiplier <= 0 {
		http.Error(w, "multiplier must be a number between 0 and 100", http.StatusBadRequest)
		return
	}
	var cardExists bool
	if err := h.DB.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM cards WHERE id = $1)", cardID).Scan(&cardExists); err != nil {
		slog.Error("Failed to validate error card", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !cardExists {
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}

	_, err = h.DB.ExecContext(r.Context(), `
		INSERT INTO error_cards (card_id, error_type, description, estimated_value_multiplier, submitted_by)
		VALUES ($1, $2, $3, $4, $5)`,
		cardID, errorType, description, multiplier, userID)
	if err != nil {
		slog.Error("Failed to submit error card", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set(
		"HX-Trigger",
		`{"notify":{"msg":"Misprint submitted for review","type":"success"}}`,
	)
	w.WriteHeader(http.StatusOK)
}
