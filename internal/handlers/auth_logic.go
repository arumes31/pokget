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
	"database/sql"
	"encoding/hex"
	"pokget/internal/auth"
	"pokget/internal/db"
	"pokget/internal/models"
	"pokget/internal/service"
	"log/slog"
	"net/http"
)

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	token := generateToken()
	_, err = db.DB.Exec("INSERT INTO users (email, password_hash, verification_token) VALUES ($1, $2, $3)", email, hash, token)
	if err != nil {
		slog.Error("Failed to register user", "error", err)
		http.Error(w, "User already exists or internal error", http.StatusConflict)
		return
	}

	mailSvc := service.NewMailService()
	if err := mailSvc.SendConfirmationEmail(email, token); err != nil {
		slog.Error("Failed to send confirmation email", "error", err)
		// We still created the user, but they'll need to re-request or we'll need a better retry logic
	}

	w.WriteHeader(http.StatusCreated)
	if err := h.Templates.ExecuteTemplate(w, "auth_success", map[string]string{"Message": "Registration successful! Please check your email to verify your account."}); err != nil {
		slog.Error("Failed to execute success template", "error", err)
	}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	var u models.User
	err := db.DB.QueryRow("SELECT id, email, password_hash, is_verified FROM users WHERE email = $1", email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsVerified)
	if err == sql.ErrNoRows || !auth.CheckPasswordHash(password, u.PasswordHash) {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !u.IsVerified {
		http.Error(w, "Please verify your email before logging in", http.StatusForbidden)
		return
	}

	session, _ := auth.Store.Get(r, "session")
	session.Values["user_id"] = u.ID
	if err := session.Save(r, w); err != nil {
		slog.Error("Failed to save session", "error", err)
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handler) ConfirmEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	res, err := db.DB.Exec("UPDATE users SET is_verified = TRUE, verification_token = NULL WHERE verification_token = $1", token)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		http.Error(w, "Invalid or expired token", http.StatusBadRequest)
		return
	}

	if err := h.Templates.ExecuteTemplate(w, "auth_success", map[string]string{"Message": "Email verified! You can now log in."}); err != nil {
		slog.Error("Failed to execute success template", "error", err)
	}
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("Failed to generate secure token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
