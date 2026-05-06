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
	"bytes"
	"context"
	"database/sql"
	"html/template"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"pokget/internal/auth"
	"pokget/internal/db"
	"pokget/internal/models"
	"pokget/internal/service"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
)

func setupTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	tmpl := template.Must(template.New("test").Parse(`
		{{define "index.html"}}index{{end}}
		{{define "dashboard.html"}}dashboard{{end}}
		{{define "centering_tool.html"}}centering{{end}}
		{{define "auth.html"}}auth{{end}}
		{{define "auth_fragment.html"}}auth_fragment{{end}}
		{{define "auth_success"}}auth_success{{end}}
		{{define "binders.html"}}binders{{end}}
		{{define "trade.html"}}trade{{end}}
		{{define "error_db.html"}}error_db{{end}}
	`))

	dbMock, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to open mock db: %v", err)
	}
	db.DB = dbMock

	h := &Handler{
		Templates:    tmpl,
		MockCards:    []models.Card{{ID: "test-id", Name: "Test Card"}},
		Audit:        service.NewAuditService(dbMock),
		Gamification: service.NewGamificationService(dbMock),
	}

	return h, mock, func() { dbMock.Close() }
}

func TestHandlers(t *testing.T) {
	t.Run("Index_Unauthenticated", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		h.Index(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", rr.Code)
		}
	})

	t.Run("Index_Authenticated", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		session, _ := auth.Store.Get(req, "session")
		session.Values["user_id"] = "test-user"
		_ = session.Save(req, rr)

		req.Header.Set("Cookie", rr.Header().Get("Set-Cookie"))
		rr = httptest.NewRecorder()

		h.Index(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Dashboard_Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/dashboard", nil)
		rr := httptest.NewRecorder()

		h.Dashboard(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("Dashboard_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/dashboard", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows([]string{"set_name", "owned_cards", "total_cards"}).
			AddRow("151", 10, 165)
		mock.ExpectQuery("SELECT").WithArgs("test-user").WillReturnRows(rows)

		mock.ExpectQuery("SELECT COALESCE").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"val"}).AddRow(decimal.NewFromFloat(100.0)))

		h.Dashboard(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("AddCardToPortfolio_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/add", strings.NewReader("card_id=test-id&notes=cool&custom_price=10.0&condition=Near+Mint&format=Raw"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO portfolio").WithArgs("test-user", "test-id", "cool", "10.0", "Near Mint", "Raw").
			WillReturnResult(sqlmock.NewResult(1, 1))

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Auth", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/auth", nil)
		rr := httptest.NewRecorder()
		h.Auth(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		req = httptest.NewRequest("GET", "/auth", nil)
		req.Header.Set("HX-Request", "true")
		rr = httptest.NewRecorder()
		h.Auth(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Register_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/register", strings.NewReader("email=test@example.com&password=pass&confirm_password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT is_verified").WillReturnError(sql.ErrNoRows)
		mock.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Mailer = &service.MockMailer{}
		h.Register(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rr.Code)
		}
	})

	t.Run("Login_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		passHash, _ := auth.HashPassword("pass")
		rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("user-123", "test@example.com", passHash, true)
		mock.ExpectQuery("SELECT id, email").WillReturnRows(rows)
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Login(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", rr.Code)
		}
	})

	t.Run("ResendVerification_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/resend", strings.NewReader("email=test@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows([]string{"last_email_sent_at", "verification_token", "is_verified"}).
			AddRow(time.Now().Add(-10*time.Minute), "token-123", false)
		mock.ExpectQuery("SELECT last_email_sent_at").WithArgs("test@example.com").WillReturnRows(rows)
		mock.ExpectExec("UPDATE users").WithArgs("test@example.com").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Mailer = &service.MockMailer{}
		h.ResendVerification(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("APIScan_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		h.Fingerprint = service.NewFingerprintService(db.DB)

		img := image.NewRGBA(image.Rect(0, 0, 10, 10))
		buf := new(bytes.Buffer)
		_ = png.Encode(buf, img)
		
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("card_image", "test.png")
		_, _ = part.Write(buf.Bytes())
		_ = writer.Close()

		req := httptest.NewRequest("POST", "/api/scan", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "set_name", "image_url", "phash"}))

		h.APIScan(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})
}
