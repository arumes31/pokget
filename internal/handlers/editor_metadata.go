// Copyright (c) 2026 arumes31
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"pokget/internal/auth"
)

type editorBinder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// EditorMetadata supplies current settings and owned binders each time a card
// editor opens, including changes made since the app shell was rendered.
func (h *Handler) EditorMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var currency string
	err := h.DB.QueryRowContext(r.Context(), "SELECT COALESCE(currency, 'EUR') FROM users WHERE id = $1", userID).Scan(&currency)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to load editor currency", "error", err)
		http.Error(w, "Unable to load editor options", http.StatusInternalServerError)
		return
	}
	if currency != "USD" {
		currency = "EUR"
	}
	symbol := "€"
	if currency == "USD" {
		symbol = "$"
	}

	rows, err := h.DB.QueryContext(r.Context(), "SELECT id, name FROM binders WHERE user_id = $1 ORDER BY name, id", userID)
	if err != nil {
		slog.Error("Failed to load editor binders", "error", err)
		http.Error(w, "Unable to load editor options", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	binders := make([]editorBinder, 0)
	for rows.Next() {
		var binder editorBinder
		if err := rows.Scan(&binder.ID, &binder.Name); err != nil {
			slog.Error("Failed to scan editor binder", "error", err)
			http.Error(w, "Unable to load editor options", http.StatusInternalServerError)
			return
		}
		binders = append(binders, binder)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Failed to read editor binders", "error", err)
		http.Error(w, "Unable to load editor options", http.StatusInternalServerError)
		return
	}

	metadata := struct {
		Currency       string         `json:"currency"`
		CurrencySymbol string         `json:"currency_symbol"`
		Binders        []editorBinder `json:"binders"`
	}{currency, symbol, binders}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		slog.Error("Failed to write editor metadata", "error", err)
	}
}
