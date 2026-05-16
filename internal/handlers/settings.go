// Copyright (c) 2026 arumes31
package handlers

import (
	"log/slog"
	"net/http"
	"pokget/internal/auth"
)

func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Settings", "method", r.Method, "url", r.URL.String())
	
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodPost {
		currency := r.FormValue("currency")
		if currency != "USD" && currency != "EUR" {
			http.Error(w, "Invalid currency", http.StatusBadRequest)
			return
		}

		_, err := h.DB.Exec("UPDATE users SET currency = $1 WHERE id = $2", currency, userID)
		if err != nil {
			slog.Error("Failed to update user currency", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("HX-Trigger", `{"notify": {"msg": "Settings Updated", "type": "success"}}`)
		// Fallthrough to render updated settings
	}

	var email string
	var currency string
	err := h.DB.QueryRow("SELECT email, currency FROM users WHERE id = $1", userID).Scan(&email, &currency)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	
	if currency == "" {
		currency = "EUR"
	}

	data := map[string]interface{}{
		"Email":    email,
		"Currency": currency,
	}

	if r.Header.Get("HX-Request") == "true" {
		if err := h.Templates.ExecuteTemplate(w, "settings", data); err != nil {
			slog.Error("Failed to execute settings template", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	h.render(w, r, "settings.html", data)
}
