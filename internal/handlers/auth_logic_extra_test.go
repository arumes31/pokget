package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRegister_HashError(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	h.passwordHasher = func(string) (string, error) { return "", errors.New("hash failed") }

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("email=new@example.com&password=pass&confirm_password=pass"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	mock.ExpectQuery("SELECT is_verified").WillReturnError(sql.ErrNoRows)

	h.Register(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

func TestRegister_TokenError(t *testing.T) {
	oldReader := randReader
	randReader = &errorReader{}
	defer func() { randReader = oldReader }()

	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("email=new@example.com&password=pass&confirm_password=pass"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	mock.ExpectQuery("SELECT is_verified").WillReturnError(sql.ErrNoRows)

	h.Register(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

func TestResendVerification_Branches(t *testing.T) {
	t.Run("MissingEmail", func(t *testing.T) {
		h, _, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/resend", nil)
		rr := httptest.NewRecorder()

		h.ResendVerification(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("AlreadyVerified", func(t *testing.T) {
		h, mock, cleanup := setupTestHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/resend", strings.NewReader("email=test@example.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()

		rows := sqlmock.NewRows([]string{"last_email_sent_at", "verification_token", "is_verified"}).
			AddRow(nil, "token-123", true)
		mock.ExpectQuery("SELECT last_email_sent_at").WithArgs("test@example.com").WillReturnRows(rows)

		h.ResendVerification(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "already verified") {
			t.Errorf("Expected already-verified message, got %q", rr.Body.String())
		}
	})
}

func TestLogin_SuccessRedirect(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	// No HX-Request header — a plain browser login must end in a 303 redirect.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("email=test@example.com&password=pass"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	passHash := fastPasswordHash(t, "pass")
	rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
		AddRow("user-123", "test@example.com", passHash, true, 0)
	mock.ExpectQuery("SELECT id, email, password_hash, is_verified").WillReturnRows(rows)

	h.Login(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rr.Code)
	}
	if location := rr.Header().Get("Location"); location != "/" {
		t.Errorf("Expected redirect to '/', got %q", location)
	}
}

func TestProcessConfirmEmail_MissingToken(t *testing.T) {
	h, _, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/confirm", nil)
	rr := httptest.NewRecorder()

	h.ProcessConfirmEmail(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}
