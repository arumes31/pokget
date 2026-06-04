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
	"github.com/gorilla/csrf"
	"pokget/internal/auth"
	"pokget/internal/errors"
	"pokget/internal/models"
	"log/slog"
	"net/http"
	"time"
)

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Register", "method", r.Method, "url", r.URL.String())
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if email == "" || password == "" || confirmPassword == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	if password != confirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}

	if err := h.AuthService.RegisterUser(r.Context(), email, password); err != nil {
		slog.Error("Registration failed", "error", err)
		http.Error(w, err.Error(), errors.MapToStatusCode(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
	data := map[string]interface{}{
		"Message":   "Registration successful! Please check your email to verify your account.",
		"CSRFToken": csrf.Token(r),
	}
	if err := h.Templates.ExecuteTemplate(w, "auth_success", data); err != nil {
		slog.Error("Failed to execute success template", "error", err)
	}
}

func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ResendVerification", "method", r.Method, "url", r.URL.String())
	email := r.FormValue("email")
	if email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	var lastSent sql.NullTime
	var token string
	var isVerified bool
	err := h.DB.QueryRow("SELECT last_email_sent_at, verification_token, is_verified FROM users WHERE email = $1", email).Scan(&lastSent, &token, &isVerified)
	if err != nil {
		if err == sql.ErrNoRows {
			// Don't leak email existence, just return OK but don't send
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if isVerified {
		http.Error(w, "Account already verified", http.StatusBadRequest)
		return
	}

	// 5 minute rate limit
	if lastSent.Valid && time.Since(lastSent.Time) < 5*time.Minute {
		http.Error(w, "Please wait 5 minutes before resending", http.StatusTooManyRequests)
		return
	}

	// Update last sent time
	_, err = h.DB.Exec("UPDATE users SET last_email_sent_at = NOW() WHERE email = $1", email)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	mailSvc := h.Mailer
	if mailSvc == nil {
		// This is a fallback, but in production h.Mailer should be set
		http.Error(w, "Mailer service unavailable", http.StatusInternalServerError)
		return
	}
	if err := mailSvc.SendConfirmationEmail(email, token); err != nil {
		slog.Error("Failed to resend confirmation email", "error", err)
		http.Error(w, "Failed to send email", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Login", "method", r.Method, "url", r.URL.String())
	email := r.FormValue("email")
	password := r.FormValue("password")

	var u models.User
	err := h.DB.QueryRow("SELECT id, email, password_hash, is_verified FROM users WHERE email = $1", email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsVerified)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Invalid email or password", http.StatusUnauthorized)
			return
		}
		slog.Error("Login: database error", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !auth.CheckPasswordHash(password, u.PasswordHash) {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	if !u.IsVerified {
		http.Error(w, "Please verify your email before logging in", http.StatusForbidden)
		return
	}

	remember := r.FormValue("remember") == "on"
	session, _ := auth.Store.Get(r, "session")
	session.Values["user_id"] = u.ID
	
	if remember {
		session.Options.MaxAge = 86400 * 30 // 30 days
	} else {
		session.Options.MaxAge = 0 // Session cookie (Expires when browser closes)
	}
	session.Options.SameSite = http.SameSiteLaxMode
	session.Options.HttpOnly = true

	if err := session.Save(r, w); err != nil {
		slog.Error("Failed to save session", "error", err)
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	h.Audit.Log(u.ID, "USER_LOGIN", map[string]interface{}{"email": u.Email})

	if r.Header.Get("HX-Request") == "true" {
		// Replace /auth in browser history so back button doesn't return to login
		w.Header().Set("HX-Redirect", "/")
		w.Header().Set("HX-Replace-Url", "/")
		w.WriteHeader(http.StatusOK)
		return
	}
	// For non-HTMX requests, replace the history entry with JS
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<script>window.location.replace("/")</script>`))
}

func (h *Handler) ConfirmEmail(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ConfirmEmail (GET)", "method", r.Method, "url", r.URL.String())
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	// Just render the confirmation page with the token
	data := map[string]interface{}{
		"Token": token,
	}
	h.render(w, r, "confirm_email.html", data)
}

func (h *Handler) ProcessConfirmEmail(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ProcessConfirmEmail (POST)", "method", r.Method, "url", r.URL.String())
	token := r.FormValue("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	res, err := h.DB.Exec("UPDATE users SET is_verified = TRUE, verification_token = NULL WHERE verification_token = $1", token)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		http.Error(w, "Invalid or expired token. Your account might already be verified.", http.StatusBadRequest)
		return
	}

	if err := h.Templates.ExecuteTemplate(w, "confirm_success", nil); err != nil {
		slog.Error("Failed to execute success template", "error", err)
	}
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	session, _ := auth.Store.Get(r, "session")
	session.Values["user_id"] = ""
	session.Options.MaxAge = -1
	_ = session.Save(r, w)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/auth")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/auth", http.StatusSeeOther)
}
