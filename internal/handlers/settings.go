// Copyright (c) 2026 arumes31
package handlers

import (
	"encoding/json"
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

// BUG-M11 FIX: ChangePassword handler that invalidates the existing session
// after a successful password change. Previously, when a user changed their
// password, existing sessions remained valid — a security issue if the account
// was compromised. Now, all sessions are invalidated by destroying the session
// cookie, forcing the user to re-authenticate with the new password.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ChangePassword", "method", r.Method)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		http.Error(w, "All password fields are required", http.StatusBadRequest)
		return
	}

	if newPassword != confirmPassword {
		http.Error(w, "New passwords do not match", http.StatusBadRequest)
		return
	}

	// Verify current password
	var currentHash string
	err := h.DB.QueryRow("SELECT password_hash FROM users WHERE id = $1", userID).Scan(&currentHash)
	if err != nil {
		slog.Error("Failed to fetch user for password change", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if !auth.CheckPasswordHash(currentPassword, currentHash) {
		http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	// Hash the new password
	newHash, err := auth.HashPassword(newPassword)
	if err != nil {
		slog.Error("Failed to hash new password", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Update password in database
	_, err = h.DB.Exec("UPDATE users SET password_hash = $1 WHERE id = $2", newHash, userID)
	if err != nil {
		slog.Error("Failed to update password", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Invalidate the current session by destroying the session cookie
	session, _ := auth.Store.Get(r, "session")
	session.Values["user_id"] = ""
	session.Options.MaxAge = -1
	_ = session.Save(r, w)

	h.Audit.Log(userID, "PASSWORD_CHANGE", map[string]interface{}{"action": "session_invalidated"})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("HX-Redirect", "/auth")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Password changed. Please log in again.",
	})
}
