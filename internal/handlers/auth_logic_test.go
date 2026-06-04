package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"pokget/internal/auth"
	"pokget/internal/service"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRegister_Success_NewUser(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/register", strings.NewReader("email=new@example.com&password=pass123&confirm_password=pass123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	// 1. Check if user exists
	mock.ExpectQuery("SELECT is_verified FROM users WHERE email = \\$1").
		WithArgs("new@example.com").
		WillReturnError(sql.ErrNoRows)

	// 2. Insert new user
	mock.ExpectExec("INSERT INTO users").
		WithArgs("new@example.com", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// 3. Audit log
	mock.ExpectExec("INSERT INTO audit_logs").
		WillReturnResult(sqlmock.NewResult(1, 1))

	h.Mailer = &service.MockMailer{}
	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", rr.Code)
	}
}

func TestRegister_PasswordMismatch(t *testing.T) {
	h, _, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/register", strings.NewReader("email=test@example.com&password=pass1&confirm_password=pass2"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Passwords do not match") {
		t.Errorf("Expected error message 'Passwords do not match', got %s", rr.Body.String())
	}
}

func TestLogout(t *testing.T) {
	h, _, cleanup := setupTestHandler(t)
	defer cleanup()

	t.Run("Standard", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/logout", nil)
		rr := httptest.NewRecorder()
		h.Logout(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", rr.Code)
		}
	})

	t.Run("HTMX", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/logout", nil)
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()
		h.Logout(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if rr.Header().Get("HX-Redirect") != "/auth" {
			t.Errorf("Expected HX-Redirect header /auth, got %s", rr.Header().Get("HX-Redirect"))
		}
	})
}

func TestLogin_Failures(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	t.Run("InvalidPassword", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		hash, _ := auth.HashPassword("correct")
		rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("u1", "test@example.com", hash, true)
		mock.ExpectQuery("SELECT id, email").WillReturnRows(rows)

		h.Login(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401, got %d", rr.Code)
		}
	})

	t.Run("Unverified", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/login", strings.NewReader("email=test@example.com&password=correct"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		hash, _ := auth.HashPassword("correct")
		rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified"}).
			AddRow("u1", "test@example.com", hash, false)
		mock.ExpectQuery("SELECT id, email").WillReturnRows(rows)

		h.Login(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected 403, got %d", rr.Code)
		}
	})
}

func TestResendVerification_Success(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/resend", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	lastSent := time.Now().Add(-10 * time.Minute)
	rows := sqlmock.NewRows([]string{"last_email_sent_at", "verification_token", "is_verified"}).
		AddRow(lastSent, "token123", false)
	mock.ExpectQuery("SELECT last_email_sent_at").WillReturnRows(rows)
	mock.ExpectExec("UPDATE users SET last_email_sent_at").WillReturnResult(sqlmock.NewResult(1, 1))

	h.Mailer = &service.MockMailer{}
	h.ResendVerification(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestConfirmEmail_Full(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	t.Run("ProcessSuccess", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/confirm", strings.NewReader("token=valid-token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users SET is_verified = TRUE").
			WithArgs("valid-token").
			WillReturnResult(sqlmock.NewResult(1, 1))

		h.ProcessConfirmEmail(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}
	})

	t.Run("ProcessInvalidToken", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/confirm", strings.NewReader("token=invalid-token"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		mock.ExpectExec("UPDATE users SET is_verified = TRUE").
			WillReturnResult(sqlmock.NewResult(0, 0))

		h.ProcessConfirmEmail(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", rr.Code)
		}
	})
}

func TestConfirmEmail_GET(t *testing.T) {
	h, _, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/confirm?token=test-token", nil)
	rr := httptest.NewRecorder()

	h.ConfirmEmail(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestResendVerification_RateLimited(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/resend", strings.NewReader("email=test@example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	lastSent := time.Now().Add(-1 * time.Minute)
	rows := sqlmock.NewRows([]string{"last_email_sent_at", "verification_token", "is_verified"}).
		AddRow(lastSent, "token123", false)
	mock.ExpectQuery("SELECT last_email_sent_at").WillReturnRows(rows)

	h.ResendVerification(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", rr.Code)
	}
}
