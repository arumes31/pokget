package handlers

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pokget/internal/auth"
	"pokget/internal/service"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRegisterRejectsPasswordBeyondBcryptLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
	}{
		{name: "ascii", password: strings.Repeat("a", 73)},
		{name: "multibyte", password: strings.Repeat("界", 25)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()
			mock.ExpectQuery("SELECT is_verified FROM users").
				WithArgs("new@example.com").WillReturnError(sql.ErrNoRows)
			form := url.Values{
				"email": {"new@example.com"}, "password": {tc.password},
				"confirm_password": {tc.password},
			}
			request := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()

			h.Register(response, request)

			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "72 bytes") {
				t.Fatalf("status = %d, body = %q; want 400 explaining the 72-byte limit", response.Code, response.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRegister_Success_NewUser(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/register", strings.NewReader("email=new@example.com&password=long-enough-pass&confirm_password=long-enough-pass"))
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

		hash := fastPasswordHash(t, "correct")
		rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
			AddRow("u1", "test@example.com", hash, true, 0)
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

		hash := fastPasswordHash(t, "correct")
		rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
			AddRow("u1", "test@example.com", hash, false, 0)
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
	rows := sqlmock.NewRows([]string{"last_email_sent_at", "is_verified"}).
		AddRow(lastSent, false)
	mock.ExpectQuery("SELECT last_email_sent_at").WillReturnRows(rows)
	mock.ExpectExec("UPDATE users SET verification_token").
		WithArgs(sqlmock.AnyArg(), "test@example.com").
		WillReturnResult(sqlmock.NewResult(1, 1))

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
			WithArgs(hashVerificationToken("valid-token")).
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
	rows := sqlmock.NewRows([]string{"last_email_sent_at", "is_verified"}).
		AddRow(lastSent, false)
	mock.ExpectQuery("SELECT last_email_sent_at").WillReturnRows(rows)

	h.ResendVerification(rr, req)

	// Rate-limited resends look like any other accepted request.
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestLogoutRejectsGET(t *testing.T) {
	handler := &Handler{}
	response := httptest.NewRecorder()
	handler.Logout(response, httptest.NewRequest(http.MethodGet, "/auth/logout", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	form := url.Values{"email": {"new@example.com"}, "password": {"short-pass1"}, "confirm_password": {"short-pass1"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "at least 12 characters") {
		t.Fatalf("status = %d, body = %q; want 400 explaining the minimum length", rr.Code, rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoginLocksAccountAfterRepeatedFailures(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	const email = "lockout@example.com"
	t.Cleanup(func() { auth.ResetLoginFailures(email) })

	hash := fastPasswordHash(t, "correct-password")
	login := func() int {
		form := url.Values{"email": {email}, "password": {"wrong-password"}}
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.Login(rr, req)
		return rr.Code
	}
	for range 10 {
		mock.ExpectQuery("SELECT id, email").WillReturnRows(
			sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
				AddRow("u1", email, hash, true, 0))
		if code := login(); code != http.StatusUnauthorized {
			t.Fatalf("failed login status = %d, want 401", code)
		}
	}
	// Locked: rejected before any database lookup or bcrypt work.
	if code := login(); code != http.StatusTooManyRequests {
		t.Fatalf("locked login status = %d, want 429", code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLogoutRevokesSessionServerSide(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	cookieRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	session, _ := auth.Store.Get(cookieRequest, "session")
	session.Values["user_id"] = "user-1"
	session.Values["session_version"] = int64(3)
	cookieResponse := httptest.NewRecorder()
	if err := session.Save(cookieRequest, cookieResponse); err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("UPDATE users SET session_version").
		WithArgs("user-1", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Cookie", cookieResponse.Header().Get("Set-Cookie"))
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
