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
	rows := sqlmock.NewRows([]string{"id", "last_email_sent_at", "is_verified"}).
		AddRow("user-123", lastSent, false)
	mock.ExpectQuery("SELECT id, last_email_sent_at").
		WithArgs("test@example.com", "test@example.com").
		WillReturnRows(rows)
	mock.ExpectExec("UPDATE users SET verification_token").
		WithArgs(sqlmock.AnyArg(), "user-123").
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
	rows := sqlmock.NewRows([]string{"id", "last_email_sent_at", "is_verified"}).
		AddRow("user-123", lastSent, false)
	mock.ExpectQuery("SELECT id, last_email_sent_at").WillReturnRows(rows)

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

func TestRegister_RejectsInvalidAndInjectionEmails(t *testing.T) {
	cases := []struct{ name, email string }{
		{"embedded CRLF", "attacker@example.com\r\nBcc: victim@example.net"},
		{"embedded LF", "attacker@example.com\nX-Injected: yes"},
		{"display name form", "Victim <victim@example.com>"},
		{"address list", "a@example.com, b@example.com"},
		{"not an address", "not-an-email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, mock, cleanup := setupTestHandler(t)
			defer cleanup()

			form := url.Values{
				"email":            {tc.email},
				"password":         {"long-enough-pass"},
				"confirm_password": {"long-enough-pass"},
			}
			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			h.Mailer = &service.MockMailer{}

			h.Register(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q", rr.Code, tc.email)
			}
			// Rejection happens before any DB lookup, hashing, or mailer call.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("no DB query should run for an invalid email: %v", err)
			}
		})
	}
}

func TestRegister_CanonicalizesEmailToLowercase(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	form := url.Values{
		"email":            {" New@Example.COM "},
		"password":         {"long-enough-pass"},
		"confirm_password": {"long-enough-pass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	// The query and insert must see the trimmed, lowercased form.
	mock.ExpectQuery("SELECT is_verified FROM users WHERE email = \\$1").
		WithArgs("new@example.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO users").
		WithArgs("new@example.com", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO audit_logs").
		WillReturnResult(sqlmock.NewResult(1, 1))
	h.Mailer = &service.MockMailer{}

	h.Register(rr, req)

	// A 201 proves the SELECT and INSERT both matched their WithArgs expectation
	// of the canonical "new@example.com": a mismatched arg would have errored the
	// query and returned 500. The audit-log insert is asynchronous, so (as in
	// TestRegister_Success_NewUser) its expectation is not verified here to avoid
	// a race with the background writer.
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (canonicalized email must match the query args)", rr.Code)
	}
}

func TestResendVerification_RejectsInjectionEmailUniformly(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	form := url.Values{"email": {"attacker@example.com\r\nBcc: victim@example.net"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/resend", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.ResendVerification(rr, req)

	// Same empty 200 as an unknown address, and no DB query or mail.
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want uniform 200", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no DB query should run for an invalid email: %v", err)
	}
}

func TestLoginLocksAccountAfterRepeatedFailures(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	const email = "lockout@example.com"
	// httptest.NewRequest sets RemoteAddr to 192.0.2.1:1234, so the lockout keys
	// on that IP; clear the pair afterwards so other tests are not affected.
	t.Cleanup(func() { auth.ResetLoginFailures(email, "192.0.2.1") })

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

func TestLogin_CanonicalizesEmail(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	t.Cleanup(func() { auth.ResetLoginFailures("test@example.com", "192.0.2.1") })

	form := url.Values{"email": {" Test@Example.COM "}, "password": {"pass"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	passHash := fastPasswordHash(t, "pass")
	// The lookup receives the raw submitted value and its canonical (trimmed,
	// lowercased) form; the canonical form matches the stored row.
	mock.ExpectQuery("SELECT id, email, password_hash, is_verified").
		WithArgs(" Test@Example.COM ", "test@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
			AddRow("user-123", "test@example.com", passHash, true, 0))

	h.Login(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (canonicalized email must match the query args)", rr.Code)
	}
}

// Finding 2(a) end to end: an attacker who locks the (IP, account) pair from
// one IP must not lock the victim out from a different IP.
func TestLoginLockoutIsScopedToClientIP(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()
	const email = "cross-ip@example.com"
	t.Cleanup(func() {
		auth.ResetLoginFailures(email, "192.0.2.1")
		auth.ResetLoginFailures(email, "198.51.100.23")
	})
	passHash := fastPasswordHash(t, "correct-password")

	fail := func() int {
		mock.ExpectQuery("SELECT id, email").WillReturnRows(
			sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
				AddRow("u1", email, passHash, true, 0))
		form := url.Values{"email": {email}, "password": {"wrong-password"}}
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.Login(rr, req) // default RemoteAddr 192.0.2.1 — the attacker IP
		return rr.Code
	}
	for range 10 {
		if code := fail(); code != http.StatusUnauthorized {
			t.Fatalf("attacker failure status = %d, want 401", code)
		}
	}
	// The attacker's IP is now locked for this account.
	if code := fail(); code != http.StatusTooManyRequests {
		t.Fatalf("attacker IP should be locked, got %d, want 429", code)
	}

	// The victim, from their OWN IP and with the correct password, is NOT locked.
	mock.ExpectQuery("SELECT id, email").WillReturnRows(
		sqlmock.NewRows([]string{"id", "email", "password_hash", "is_verified", "session_version"}).
			AddRow("u1", email, passHash, true, 0))
	form := url.Values{"email": {email}, "password": {"correct-password"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.23:4000"
	rr := httptest.NewRecorder()
	h.Login(rr, req)
	if rr.Code == http.StatusTooManyRequests {
		t.Fatalf("victim locked out from their own IP (got 429); lockout is not IP-scoped")
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

func TestLogout_RevokeError(t *testing.T) {
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
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Cookie", cookieResponse.Header().Get("Set-Cookie"))
	rr := httptest.NewRecorder()
	h.Logout(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRegister_ConcurrentConflict(t *testing.T) {
	h, mock, cleanup := setupTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/register", strings.NewReader("email=conflict@example.com&password=long-enough-pass&confirm_password=long-enough-pass"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	// Initial check reports no rows
	mock.ExpectQuery("SELECT is_verified FROM users WHERE email = \\$1").
		WithArgs("conflict@example.com").
		WillReturnError(sql.ErrNoRows)

	// Concurrent insert passes ON CONFLICT returning 0 rows affected
	mock.ExpectExec("INSERT INTO users").
		WithArgs("conflict@example.com", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	h.Mailer = &service.MockMailer{}
	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("Expected status 201 on concurrent registration, got %d", rr.Code)
	}
}
