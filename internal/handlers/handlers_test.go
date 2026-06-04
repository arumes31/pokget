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
	"errors"
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
	"github.com/gorilla/mux"
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
		{{define "error_database.html"}}error_db{{end}}
		{{define "wantlist.html"}}wantlist{{end}}
		{{define "public_vault.html"}}public_vault{{end}}
		{{define "confirm_email.html"}}confirm_email{{end}}
		{{define "confirm_success"}}confirm_success{{end}}
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
		Game:         service.NewGamificationService(dbMock),
		Fingerprint:  service.NewFingerprintService(dbMock),
		AuthService:  service.NewAuthService(dbMock, service.NewMailService(), service.NewAuditService(dbMock)),
		DB:           dbMock,
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

		mock.ExpectQuery("SELECT COUNT").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))
		mock.ExpectQuery("SELECT SUM").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(100.50))
		mock.ExpectQuery("SELECT id").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"id", "binder_id", "name", "set", "price", "image", "game"}).
			AddRow("1", "b1", "Pikachu", "Base", 50.0, "url", "Pokemon"))
		mock.ExpectQuery("SELECT xp").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"xp", "rank"}).AddRow(100, "Novice"))

		h.Dashboard(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("AddCardToPortfolio_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/add", strings.NewReader("card_id=test-id&condition=Near+Mint"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO portfolio").WillReturnError(sql.ErrConnDone)

		h.AddCardToPortfolio(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("PublicVault_NotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/vault/notfound", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "notfound"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WithArgs("notfound").WillReturnError(sql.ErrNoRows)

		h.PublicVault(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
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
	})

	t.Run("Register_ReRegister", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/register", strings.NewReader("email=test@example.com&password=pass&confirm_password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		// New logic uses UPSERT
		mock.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Mailer = &service.MockMailer{}
		h.Register(rr, req)

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rr.Code)
		}
	})

	t.Run("Login_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		// Return a real DB error, not sql.ErrNoRows
		mock.ExpectQuery("SELECT id").WillReturnError(sql.ErrConnDone)

		h.Login(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Register_DBErrorRow", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/register", strings.NewReader("email=test@example.com&password=pass&confirm_password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO users").WillReturnError(errors.New("query fail"))

		h.Register(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("ResendVerification_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/resend", strings.NewReader("email=test@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT last_email_sent_at").WillReturnError(errors.New("db error"))

		h.ResendVerification(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Register_Conflict", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/register", strings.NewReader("email=test@example.com&password=pass&confirm_password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(0, 0))

		h.Register(rr, req)

		if rr.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", rr.Code)
		}
	})

	t.Run("Register_MissingFields", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/register", nil)
		rr := httptest.NewRecorder()

		h.Register(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("Login_Unverified", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		password := "pass"
		hash, _ := auth.HashPassword(password)

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password="+password))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("test-user", "test@example.com", hash, false))

		h.Login(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rr.Code)
		}
	})

	t.Run("Login_InvalidCreds", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		hash, _ := auth.HashPassword("correct")
		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("test-user", "test@example.com", hash, true))

		h.Login(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("ConfirmEmail_Invalid", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/confirm", strings.NewReader("token=invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users").WillReturnResult(sqlmock.NewResult(0, 0))

		h.ProcessConfirmEmail(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("ConfirmEmail_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/confirm", strings.NewReader("token=token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users").WillReturnError(errors.New("db error"))

		h.ProcessConfirmEmail(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Heartbeat_Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/heartbeat", nil)
		rr := httptest.NewRecorder()

		h.Heartbeat(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("ToggleVisibility_Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/visibility", nil)
		rr := httptest.NewRecorder()

		h.ToggleVisibility(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_Unauthorized", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", nil)
		rr := httptest.NewRecorder()

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("Login_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		password := "pass"
		hash, _ := auth.HashPassword(password)

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password="+password))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("test-user", "test@example.com", hash, true))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Login(rr, req)

		// Check for redirect or success (depends on HTMX header)
		if rr.Code != http.StatusOK { // Since we're writing JS redirect in test
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("ResendVerification_UserNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/resend", strings.NewReader("email=notfound@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT last_email_sent_at").WillReturnError(sql.ErrNoRows)

		h.ResendVerification(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200 (for privacy), got %d", rr.Code)
		}
	})

	t.Run("ResendVerification_MailFail", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/resend", strings.NewReader("email=test@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT last_email_sent_at").WillReturnRows(sqlmock.NewRows([]string{"last_email_sent_at", "verification_token", "is_verified"}).
			AddRow(time.Now().Add(-10*time.Minute), "token123", false))
		mock.ExpectExec("UPDATE users").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Mailer = &service.MockMailer{Err: errors.New("mail fail")}
		h.ResendVerification(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Login_UserNotFound", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=missing@example.com&password=any"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WillReturnError(sql.ErrNoRows)

		h.Login(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("APIScan_BadFile", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("card_image", "test.txt")
		_, _ = part.Write([]byte("not an image"))
		_ = writer.Close()

		req := httptest.NewRequest("POST", "/api/scan", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		h.APIScan(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Register_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/register", strings.NewReader("email=test@example.com&password=pass&confirm_password=pass"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO users").WillReturnError(sql.ErrConnDone)

		h.Register(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("Login_InvalidHash", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password=any"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("user-1", "test@example.com", "invalid-hash", true))

		h.Login(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("Render_Error", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		h.render(rr, req, "nonexistent.html", nil)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("ConfirmEmail_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/confirm", strings.NewReader("token=valid-token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users").WithArgs("valid-token").WillReturnResult(sqlmock.NewResult(1, 1))

		h.ProcessConfirmEmail(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Heartbeat", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/heartbeat", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT xp").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"xp", "rank"}).AddRow(100, "Novice"))
		mock.ExpectExec("UPDATE users").WillReturnResult(sqlmock.NewResult(1, 1))

		h.Heartbeat(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("EditPortfolioItem_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/edit", strings.NewReader("item_id=123&custom_price=50.0&grade=10&notes=updated&is_public=true"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnResult(sqlmock.NewResult(1, 1))

		h.EditPortfolioItem(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("PublicVault", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/vault/test-user", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "test-user"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id").WithArgs("test-user").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "rank", "xp"}).
			AddRow("u1", "test@example.com", "Novice", 100))
		mock.ExpectQuery("SELECT p.id").WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"id", "cond", "price", "cid", "name", "set", "img", "pusd", "game"}).
			AddRow("p1", "NM", 10.0, "c1", "Mew", "151", "url", 15.0, "Pokemon"))

		h.PublicVault(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("ToggleVisibility_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/visibility", strings.NewReader("item_id=1&is_public=true"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio SET is_public").WillReturnResult(sqlmock.NewResult(1, 1))

		h.ToggleVisibility(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Wantlist", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/wantlist", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "tp", "notes", "name", "set", "price", "url"}).
				AddRow("1", "c1", 10.0, "note", "Mew", "151", 15.0, "url"))

		h.Wantlist(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("EditPortfolioItem_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/edit", strings.NewReader("item_id=123"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio").WillReturnError(sql.ErrConnDone)

		h.EditPortfolioItem(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("EditPortfolioItem_MissingID", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/edit", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.EditPortfolioItem(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("Heartbeat_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/heartbeat", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT xp").WillReturnError(sql.ErrConnDone)

		h.Heartbeat(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("PublicVault_PrivateVault", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/vault/test-user", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "test-user"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, email, rank_title, xp").WithArgs("test-user").
			WillReturnError(sql.ErrNoRows)

		h.PublicVault(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("PublicVault_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/vault/test-user", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "test-user"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, email, rank_title, xp").WithArgs("test-user").
			WillReturnError(errors.New("db error"))

		h.PublicVault(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_MissingCard", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("Centering", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/centering", nil)
		rr := httptest.NewRecorder()

		h.Centering(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Binders", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/binders", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT b.id").WithArgs("test-user").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "desc", "created", "count"}).
				AddRow("b1", "Test Binder", "Desc", "2026-01-01", 5))

		h.Binders(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("Trade", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/trade", nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		h.Trade(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("ErrorDatabase", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/error", nil)
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id", "cid", "type", "desc", "mult", "name", "set", "url", "game"}).
			AddRow("1", "c1", "Misprint", "Blurry", 2.0, "Pikachu", "Base", "url", "Pokemon"))

		h.ErrorDatabase(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("SubmitError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/error/submit", strings.NewReader("card_id=c1&error_type=Miscut&description=offcenter&multiplier=1.5"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO error_cards").WithArgs("c1", "Miscut", "offcenter", "1.5", "test-user").
			WillReturnResult(sqlmock.NewResult(1, 1))

		h.SubmitError(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("ConfirmEmail_MissingToken", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/confirm", nil)
		rr := httptest.NewRecorder()

		h.ConfirmEmail(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("PublicVault_Private", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/vault/test-user", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "test-user"})
		rr := httptest.NewRecorder()

		mock.ExpectQuery("SELECT id, email, rank_title, xp").WithArgs("test-user").
			WillReturnError(sql.ErrNoRows)

		h.PublicVault(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", rr.Code)
		}
	})

	t.Run("ToggleVisibility_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/portfolio/visibility", strings.NewReader("item_id=1&is_public=true"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE portfolio SET is_public").WillReturnError(sql.ErrConnDone)

		h.ToggleVisibility(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("AddToWantlist_DBError", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("POST", "/wantlist/add", strings.NewReader("card_id=c1&target_price=10.0"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ctx := context.WithValue(req.Context(), auth.UserContextKey{}, "test-user")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()

		mock.ExpectExec("INSERT INTO wantlist").WillReturnError(sql.ErrConnDone)

		h.AddToWantlist(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})

	t.Run("APIScan_Success", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

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

	t.Run("Render_Error", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		// Request a template that doesn't exist
		h.render(rr, req, "nonexistent.html", nil)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}
	})
}


