// Copyright (c) 2026 arumes31
package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"pokget/internal/auth"

	"github.com/gorilla/csrf"
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

		_, err := h.DB.ExecContext(r.Context(), "UPDATE users SET currency = $1 WHERE id = $2", currency, userID)
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
	var publicSlug string
	var isPublicProfile bool
	err := h.DB.QueryRowContext(r.Context(), "SELECT email, COALESCE(currency, 'EUR'), COALESCE(public_slug, ''), COALESCE(is_public_profile, FALSE) FROM users WHERE id = $1", userID).
		Scan(&email, &currency, &publicSlug, &isPublicProfile)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to load settings", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if currency == "" {
		currency = "EUR"
	}

	data := map[string]interface{}{
		"Email":           email,
		"Currency":        currency,
		"CSRFToken":       csrf.Token(r),
		"PublicSlug":      publicSlug,
		"IsPublicProfile": isPublicProfile,
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

func (h *Handler) UpdatePublicProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := r.Context().Value(auth.UserContextKey{}).(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	enabled := r.FormValue("is_public_profile") == "true"
	var slug string
	if err := h.DB.QueryRowContext(r.Context(), "SELECT COALESCE(public_slug, '') FROM users WHERE id = $1", userID).Scan(&slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to load public profile settings", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if enabled && slug == "" {
		random := make([]byte, 8)
		if _, err := rand.Read(random); err != nil {
			slog.Error("Failed to generate public profile slug", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		slug = "collector-" + hex.EncodeToString(random)
	}
	result, err := h.DB.ExecContext(r.Context(), "UPDATE users SET public_slug = NULLIF($1, ''), is_public_profile = $2 WHERE id = $3", slug, enabled, userID)
	if err != nil {
		slog.Error("Failed to update public profile", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	w.Header().Set("HX-Trigger", `{"notify": {"msg": "Public profile updated", "type": "success"}}`)
	renderRequest := r.Clone(r.Context())
	renderRequest.Method = http.MethodGet
	h.Settings(w, renderRequest)
}

// ChangePassword updates the password, increments the server-side session
// version, and expires the current browser cookie. Every previously issued
// cookie then fails session-version validation.
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

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Error("Failed to begin password change", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Lock the account so two concurrent password changes cannot both validate
	// the same old password and race to install different new credentials.
	var currentHash string
	err = tx.QueryRowContext(
		r.Context(),
		"SELECT password_hash FROM users WHERE id = $1 FOR UPDATE",
		userID,
	).Scan(&currentHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		slog.Error("Failed to fetch user for password change", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if !auth.CheckPasswordHash(currentPassword, currentHash) {
		http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	// Hash the new password
	newHash, err := h.hashPassword(newPassword)
	if err != nil {
		slog.Error("Failed to hash new password", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	var newSessionVersion int64
	err = tx.QueryRowContext(
		r.Context(),
		`UPDATE users
		 SET password_hash = $1, session_version = COALESCE(session_version, 0) + 1
		 WHERE id = $2
		 RETURNING session_version`,
		newHash,
		userID,
	).Scan(&newSessionVersion)
	if err != nil {
		slog.Error("Failed to update password", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("Failed to commit password change", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Invalidate the current session by destroying the session cookie
	session, err := auth.Store.Get(r, "session")
	if err != nil {
		slog.Error("Failed to load session after password change", "error", err)
		http.Error(w, "Password changed; please log in again", http.StatusInternalServerError)
		return
	}
	session.Values["user_id"] = ""
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		slog.Error("Failed to invalidate session after password change", "error", err)
		http.Error(w, "Password changed; please clear your session and log in again", http.StatusInternalServerError)
		return
	}

	h.Audit.Log(userID, "PASSWORD_CHANGE", map[string]interface{}{
		"action":          "all_sessions_invalidated",
		"session_version": newSessionVersion,
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("HX-Redirect", "/auth")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Password changed. Please log in again.",
	})
}
