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
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"pokget/internal/auth"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"golang.org/x/crypto/bcrypt"
)

const passwordTooShortMessage = "Password must be at least 12 characters"

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Register", "method", r.Method, "path", r.URL.Path)
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

	if err := auth.ValidateNewPassword(password); err != nil {
		http.Error(w, passwordTooShortMessage, http.StatusBadRequest)
		return
	}

	var existingVerified bool
	err := h.DB.QueryRowContext(r.Context(), "SELECT is_verified FROM users WHERE email = $1", email).Scan(&existingVerified)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("Failed to look up user for registration", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Hash even for existing accounts so response time does not reveal
	// whether the email is registered.
	hash, errHash := h.hashPassword(password)
	if errHash != nil {
		if errors.Is(errHash, bcrypt.ErrPasswordTooLong) {
			http.Error(w, "Password must be at most 72 bytes; non-ASCII characters may use multiple bytes", http.StatusBadRequest)
			return
		}
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	token, errToken := generateToken()
	if errToken != nil {
		slog.Error("Failed to generate verification token", "error", errToken)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Existing accounts (verified or not) keep their credentials and get the
	// same response as a new registration, so the form cannot be used to
	// enumerate emails. Unverified users can use "Resend email".
	if errors.Is(err, sql.ErrNoRows) {
		res, execErr := h.DB.ExecContext(r.Context(), "INSERT INTO users (email, password_hash, verification_token, verification_expires_at, last_email_sent_at) VALUES ($1, $2, $3, NOW() + INTERVAL '24 hours', NOW()) ON CONFLICT (email) DO NOTHING", email, hash, hashVerificationToken(token))
		if execErr != nil {
			errStr := strings.ToLower(execErr.Error())
			if strings.Contains(errStr, "unique") || strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "23505") {
				slog.Info("Concurrent registration for existing email", "email", email)
			} else {
				slog.Error("Failed to register user", "error", execErr)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
		} else {
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected > 0 {
				mailSvc := h.Mailer
				if mailSvc == nil {
					mailSvc = service.NewMailService()
				}
				go func(svc service.Mailer, to, tok string) {
					if err := svc.SendConfirmationEmail(to, tok); err != nil {
						slog.Error("Failed to send confirmation email", "error", err)
					}
				}(mailSvc, email, token)

				h.Audit.Log("", "USER_REGISTER", map[string]interface{}{"email": email})
			}
		}
	}

	w.WriteHeader(http.StatusCreated)
	data := map[string]interface{}{
		"Message":   "Registration successful! Please check your email to verify your account.",
		"Email":     email,
		"CSRFToken": csrf.Token(r),
	}
	if err := h.Templates.ExecuteTemplate(w, "auth_success", data); err != nil {
		slog.Error("Failed to execute success template", "error", err)
	}
}

func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ResendVerification", "method", r.Method, "path", r.URL.Path)
	email := r.FormValue("email")
	if email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	// Unknown, already verified, and recently mailed accounts all get the same
	// empty 200 so the endpoint does not reveal which emails are registered.
	var lastSent sql.NullTime
	var isVerified bool
	err := h.DB.QueryRowContext(r.Context(), "SELECT last_email_sent_at, is_verified FROM users WHERE email = $1", email).Scan(&lastSent, &isVerified)
	if err != nil {
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// 5 minute rate limit
	if isVerified || (lastSent.Valid && time.Since(lastSent.Time) < 5*time.Minute) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only the token hash is stored, so a resend issues a fresh token.
	token, err := generateToken()
	if err != nil {
		slog.Error("Failed to generate verification token", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	_, err = h.DB.ExecContext(r.Context(), "UPDATE users SET verification_token = $1, verification_expires_at = NOW() + INTERVAL '24 hours', last_email_sent_at = NOW() WHERE email = $2 AND is_verified = FALSE", hashVerificationToken(token), email)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	mailSvc := h.Mailer
	if mailSvc == nil {
		mailSvc = service.NewMailService()
	}
	go func(svc service.Mailer, to, tok string) {
		if err := svc.SendConfirmationEmail(to, tok); err != nil {
			slog.Error("Failed to resend confirmation email", "error", err)
		}
	}(mailSvc, email, token)

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: Login", "method", r.Method, "path", r.URL.Path)
	email := r.FormValue("email")
	password := r.FormValue("password")

	// ⚡ Bolt Optimization: Early return for empty credentials to avoid unnecessary DB query.
	if email == "" || password == "" {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}
	if !auth.LoginAllowed(email) {
		http.Error(w, "Too many failed login attempts. Try again later.", http.StatusTooManyRequests)
		return
	}
	var u models.User
	var sessionVersion int64
	err := h.DB.QueryRowContext(
		r.Context(),
		"SELECT id, email, password_hash, is_verified, COALESCE(session_version, 0) FROM users WHERE email = $1",
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsVerified, &sessionVersion)
	if err != nil {
		if err == sql.ErrNoRows {
			// Spend the same bcrypt work as a real check so timing does not
			// reveal whether the email is registered.
			auth.EqualizePasswordCheck(password)
			auth.RecordLoginFailure(email)
			http.Error(w, "Invalid email or password", http.StatusUnauthorized)
			return
		}
		slog.Error("Login: database error", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !auth.CheckPasswordHash(password, u.PasswordHash) {
		auth.RecordLoginFailure(email)
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}
	auth.ResetLoginFailures(email)

	if !u.IsVerified {
		http.Error(w, "Please verify your email before logging in", http.StatusForbidden)
		return
	}

	remember := r.FormValue("remember") == "on"
	// An undecodable old cookie still yields a fresh session to log into.
	session, err := auth.Store.Get(r, "session")
	if session == nil {
		slog.Error("Failed to get session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	session.Values["user_id"] = u.ID
	session.Values["session_version"] = sessionVersion

	if remember {
		session.Options.MaxAge = 86400 * 30 // 30 days
	} else {
		session.Options.MaxAge = 0 // Session cookie (Expires when browser closes)
	}
	session.Options.SameSite = http.SameSiteLaxMode
	session.Options.HttpOnly = true
	// BUG-C03 FIX: Make Secure flag configurable based on environment.
	// Previously hardcoded to true, which broke login over HTTP (e.g. local development).
	// Now uses h.SecureCookies which is set from config at startup.
	session.Options.Secure = h.SecureCookies

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
	// For non-HTMX requests, use HTTP redirect
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) ConfirmEmail(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Action: ConfirmEmail (GET)", "method", r.Method, "path", r.URL.Path)
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
	slog.Debug("Action: ProcessConfirmEmail (POST)", "method", r.Method, "path", r.URL.Path)
	token := r.FormValue("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	res, err := h.DB.ExecContext(r.Context(), "UPDATE users SET is_verified = TRUE, verification_token = NULL, verification_expires_at = NULL WHERE verification_token = $1 AND verification_expires_at > NOW()", hashVerificationToken(token))
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

var randReader = rand.Reader

// BUG-L02 FIX: generateToken now returns an error instead of panicking when
// the cryptographic random reader fails. Previously, a failed rand.Read would
// crash the entire server. Now the caller can handle the error gracefully.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := randReader.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hashVerificationToken returns the form stored in users.verification_token;
// the plaintext token only ever appears in the emailed link.
func hashVerificationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Bump session_version so copies of this cookie stop working too. This
	// signs the user out on every device, as a password change does.
	if err := auth.RevokeSession(h.DB, r); err != nil {
		slog.Error("Failed to revoke session on logout", "error", err)
		http.Error(w, "Failed to revoke session", http.StatusInternalServerError)
		return
	}
	session, _ := auth.Store.Get(r, "session")
	session.Values["user_id"] = ""
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		slog.Error("Failed to save session on logout", "error", err)
		// Continue anyway — the session values are cleared in memory
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/auth")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/auth", http.StatusSeeOther)
}
