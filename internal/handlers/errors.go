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

	// Bolt: Use QueryContext with r.Context() to ensure the query is cancelled if the request is aborted.
	// This prevents resource leakage on the database server.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT e.id, e.card_id, e.error_type, e.description, e.estimated_value_multiplier, 
		       c.name, c.set_name, c.image_url, c.game
		FROM error_cards e
		JOIN cards c ON e.card_id = c.id
		ORDER BY e.created_at DESC`)

	if err != nil {
		slog.Error("Failed to fetch error database", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var errors []ErrorCard
	for rows.Next() {
		var e ErrorCard
		if err := rows.Scan(&e.ID, &e.CardID, &e.ErrorType, &e.Description, &e.EstimatedValueMultiplier, &e.CardName, &e.SetName, &e.ImageURL, &e.Game); err == nil {
			errors = append(errors, e)
		}
	}

	h.render(w, r, "error_database.html", map[string]interface{}{
		"Errors": errors,
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

	cardID := r.FormValue("card_id")
	errorType := r.FormValue("error_type")
	description := r.FormValue("description")
	multiplier := r.FormValue("multiplier")

	// Bolt: Use ExecContext with r.Context() to ensure the operation is cancelled if the request is aborted.
	// This helps in freeing up database resources early if the client disconnects.
	_, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO error_cards (card_id, error_type, description, estimated_value_multiplier, submitted_by)
		VALUES ($1, $2, $3, $4, $5)`,
		cardID, errorType, description, multiplier, userID)
	if err != nil {
		slog.Error("Failed to submit error card", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Error card submitted! Review in progress."))
}
