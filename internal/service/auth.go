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

package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/http"
	"pokget/internal/auth"
	"pokget/internal/errors"
)

type AuthService struct {
	db     *sql.DB
	mailer Mailer
	audit  *AuditService
}

func NewAuthService(db *sql.DB, mailer Mailer, audit *AuditService) *AuthService {
	return &AuthService{
		db:     db,
		mailer: mailer,
		audit:  audit,
	}
}

// RegisterUser handles the user registration process, including password hashing,
// token generation, database persistence, and sending the confirmation email.
//
// Performance Optimization:
// This implementation uses a single SQL query with ON CONFLICT to handle both
// new user creation and updating unverified users in a single database round-trip.
// This reduces latency by avoiding a preliminary SELECT query.
// Expected Impact: ~50% reduction in database-related latency for registration.
func (s *AuthService) RegisterUser(ctx context.Context, email, password string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return errors.Wrap(http.StatusInternalServerError, "Failed to hash password", err)
	}

	token := s.generateToken()

	// One-shot UPSERT with conditional update.
	// If the user exists and is verified, the WHERE clause will prevent the update,
	// and the query will return zero rows.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (email, password_hash, verification_token, last_email_sent_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (email) DO UPDATE
		SET password_hash = EXCLUDED.password_hash,
		    verification_token = EXCLUDED.verification_token,
		    last_email_sent_at = NOW()
		WHERE users.is_verified = FALSE
	`, email, hash, token)

	if err != nil {
		return errors.Wrap(http.StatusInternalServerError, "Failed to register/update user", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		// If no rows were affected, it means the user already exists and is verified.
		return errors.Wrap(http.StatusConflict, "User already exists", nil)
	}

	if err := s.mailer.SendConfirmationEmail(email, token); err != nil {
		slog.Error("Failed to send confirmation email", "error", err)
	}

	s.audit.Log("", "USER_REGISTER", map[string]interface{}{"email": email})

	return nil
}

func (s *AuthService) generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("Failed to generate secure token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
