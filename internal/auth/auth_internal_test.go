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

package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// newSessionCookieRequest returns a request carrying a valid session cookie
// with the given values stored in it.
func newSessionCookieRequest(t *testing.T, values map[interface{}]interface{}) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieResponse := httptest.NewRecorder()
	session, err := Store.Get(req, "session")
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	for key, value := range values {
		session.Values[key] = value
	}
	if err := session.Save(req, cookieResponse); err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}
	req.Header.Set("Cookie", cookieResponse.Header().Get("Set-Cookie"))
	return req
}

func TestSessionVersion(t *testing.T) {
	tests := []struct {
		name   string
		value  interface{}
		want   int64
		wantOK bool
	}{
		{"Nil", nil, 0, true},
		{"Int", int(2), 2, true},
		{"Int32", int32(3), 3, true},
		{"Int64", int64(4), 4, true},
		{"String", "not-a-version", 0, false},
		{"Float", float64(1.5), 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sessionVersion(tc.value)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("sessionVersion(%v) = (%d, %v), want (%d, %v)", tc.value, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestAuthenticateRequest(t *testing.T) {
	t.Run("NilDatabase", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, err := authenticateRequest(nil, req); !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("Expected sql.ErrConnDone, got %v", err)
		}
	})

	t.Run("UnknownUser", func(t *testing.T) {
		database, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })

		req := newSessionCookieRequest(t, map[interface{}]interface{}{"user_id": "ghost-user"})
		mock.ExpectQuery("SELECT COALESCE\\(session_version, 0\\)").
			WithArgs("ghost-user").
			WillReturnRows(sqlmock.NewRows([]string{"session_version"}))

		if _, err := authenticateRequest(database, req); !errors.Is(err, errInvalidSession) {
			t.Errorf("Expected errInvalidSession for unknown user, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("InvalidVersionType", func(t *testing.T) {
		database, _, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })

		req := newSessionCookieRequest(t, map[interface{}]interface{}{
			"user_id":         "test-user",
			"session_version": "not-a-number",
		})

		if _, err := authenticateRequest(database, req); !errors.Is(err, errInvalidSession) {
			t.Errorf("Expected errInvalidSession for invalid session_version type, got %v", err)
		}
	})
}

func TestMiddlewareNilDatabase(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("next handler should not be called")
	})
	middleware := Middleware(nil)(nextHandler)

	req := newSessionCookieRequest(t, map[interface{}]interface{}{"user_id": "test-user"})
	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 with nil database, got %d", rr.Code)
	}
}

func TestExpireSession_SaveError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	// Store.Get caches the session on the request, so a second Get inside
	// expireSession sees the unencodable value and Save fails.
	session, err := Store.Get(req, "session")
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	session.Values["user_id"] = "test-user"
	session.Values["unencodable"] = make(chan int)

	// Must not panic or write a response; the failure is only logged.
	expireSession(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected no response write, got status %d", rr.Code)
	}
}

func TestAPIAuthMiddleware(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := r.Context().Value(UserContextKey{}).(string)
		if !ok || userID != "api-user" {
			t.Errorf("Expected user ID 'api-user' in context, got %v", r.Context().Value(UserContextKey{}))
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := APIAuthMiddleware(database)(nextHandler)

	t.Run("WithValidSession", func(t *testing.T) {
		req := newSessionCookieRequest(t, map[interface{}]interface{}{
			"user_id":         "api-user",
			"session_version": int64(7),
		})
		mock.ExpectQuery("SELECT COALESCE\\(session_version, 0\\)").
			WithArgs("api-user").
			WillReturnRows(sqlmock.NewRows([]string{"session_version"}).AddRow(7))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Errorf("Expected status 204, got %d", rr.Code)
		}
	})

	t.Run("RevokedSession", func(t *testing.T) {
		req := newSessionCookieRequest(t, map[interface{}]interface{}{
			"user_id":         "api-user",
			"session_version": int64(1),
		})
		mock.ExpectQuery("SELECT COALESCE\\(session_version, 0\\)").
			WithArgs("api-user").
			WillReturnRows(sqlmock.NewRows([]string{"session_version"}).AddRow(2))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", rr.Code)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		req := newSessionCookieRequest(t, map[interface{}]interface{}{"user_id": "api-user"})
		mock.ExpectQuery("SELECT COALESCE\\(session_version, 0\\)").
			WithArgs("api-user").
			WillReturnError(sql.ErrConnDone)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status 503, got %d", rr.Code)
		}
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminMiddleware(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := AdminMiddleware(database)(nextHandler)

	requestWithUser := func(userID interface{}) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		ctx := context.WithValue(req.Context(), UserContextKey{}, userID)
		return req.WithContext(ctx)
	}

	t.Run("NoUserInContext", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin", nil))
		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rr.Code)
		}
	})

	t.Run("EmptyUserID", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, requestWithUser(""))
		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rr.Code)
		}
	})

	t.Run("NonAdmin", func(t *testing.T) {
		mock.ExpectQuery("SELECT COALESCE\\(is_admin, FALSE\\)").
			WithArgs("regular-user").
			WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(false))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, requestWithUser("regular-user"))
		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rr.Code)
		}
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mock.ExpectQuery("SELECT COALESCE\\(is_admin, FALSE\\)").
			WithArgs("regular-user").
			WillReturnError(sql.ErrConnDone)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, requestWithUser("regular-user"))
		if rr.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", rr.Code)
		}
	})

	t.Run("Admin", func(t *testing.T) {
		mock.ExpectQuery("SELECT COALESCE\\(is_admin, FALSE\\)").
			WithArgs("admin-user").
			WillReturnRows(sqlmock.NewRows([]string{"is_admin"}).AddRow(true))

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, requestWithUser("admin-user"))
		if rr.Code != http.StatusNoContent {
			t.Errorf("Expected status 204, got %d", rr.Code)
		}
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestScanRateLimitMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := ScanRateLimitMiddleware(nextHandler)

	req := httptest.NewRequest(http.MethodPost, "/scan", nil)
	req.RemoteAddr = "10.0.0.1:1234"

	// Default scan limiter allows a burst of 2.
	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("Request %d: expected status 200, got %d", i+1, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") != "5" {
		t.Errorf("Expected Retry-After header '5', got %q", rr.Header().Get("Retry-After"))
	}
}

func TestGetLimiterEnvConfig(t *testing.T) {
	t.Run("ValidEnv", func(t *testing.T) {
		t.Setenv("RATE_LIMIT", "0.001")
		t.Setenv("BURST_LIMIT", "1")

		limiter := getLimiter("10.99.0.1")
		if !limiter.Allow() {
			t.Error("First request should be allowed (burst 1)")
		}
		if limiter.Allow() {
			t.Error("Second request should be rejected with BURST_LIMIT=1 and a tiny rate")
		}
	})

	t.Run("InvalidEnv", func(t *testing.T) {
		t.Setenv("RATE_LIMIT", "not-a-number")
		t.Setenv("BURST_LIMIT", "also-not-a-number")

		// Falls back to defaults: burst of 5.
		limiter := getLimiter("10.99.0.2")
		for i := 0; i < 5; i++ {
			if !limiter.Allow() {
				t.Fatalf("Request %d should be allowed with default burst 5", i+1)
			}
		}
		if limiter.Allow() {
			t.Error("Sixth request should be rejected with default burst 5")
		}
	})

	t.Run("NonPositiveEnv", func(t *testing.T) {
		t.Setenv("RATE_LIMIT", "-2")
		t.Setenv("BURST_LIMIT", "0")

		// Non-positive values are rejected; defaults apply.
		limiter := getLimiter("10.99.0.3")
		for i := 0; i < 5; i++ {
			if !limiter.Allow() {
				t.Fatalf("Request %d should be allowed with default burst 5", i+1)
			}
		}
		if limiter.Allow() {
			t.Error("Sixth request should be rejected with default burst 5")
		}
	})
}

func TestGetScanLimiterEnvConfig(t *testing.T) {
	t.Run("ValidEnv", func(t *testing.T) {
		t.Setenv("SCAN_RATE_LIMIT", "0.001")
		t.Setenv("SCAN_BURST_LIMIT", "1")

		limiter := getScanLimiter("10.99.0.11")
		if !limiter.Allow() {
			t.Error("First request should be allowed (burst 1)")
		}
		if limiter.Allow() {
			t.Error("Second request should be rejected with SCAN_BURST_LIMIT=1 and a tiny rate")
		}
	})

	t.Run("InvalidEnv", func(t *testing.T) {
		t.Setenv("SCAN_RATE_LIMIT", "bogus")
		t.Setenv("SCAN_BURST_LIMIT", "-3")

		// Falls back to defaults: burst of 2.
		limiter := getScanLimiter("10.99.0.12")
		for i := 0; i < 2; i++ {
			if !limiter.Allow() {
				t.Fatalf("Request %d should be allowed with default burst 2", i+1)
			}
		}
		if limiter.Allow() {
			t.Error("Third request should be rejected with default burst 2")
		}
	})
}

func TestEvictStaleLimiters(t *testing.T) {
	now := time.Now()
	stale := now.Add(-2 * time.Hour)

	getLimiter("203.0.113.1")
	getLimiter("203.0.113.2")
	getScanLimiter("203.0.113.3")
	getScanLimiter("203.0.113.4")

	mu.Lock()
	limiters["203.0.113.1"].lastSeen = stale
	limiters["203.0.113.2"].lastSeen = now
	mu.Unlock()

	scanMu.Lock()
	scanLimiters["203.0.113.3"].lastSeen = stale
	scanLimiters["203.0.113.4"].lastSeen = now
	scanMu.Unlock()

	evictStaleLimiters(now)

	mu.Lock()
	if _, exists := limiters["203.0.113.1"]; exists {
		t.Error("Stale login limiter should have been evicted")
	}
	if _, exists := limiters["203.0.113.2"]; !exists {
		t.Error("Fresh login limiter should have been kept")
	}
	mu.Unlock()

	scanMu.Lock()
	if _, exists := scanLimiters["203.0.113.3"]; exists {
		t.Error("Stale scan limiter should have been evicted")
	}
	if _, exists := scanLimiters["203.0.113.4"]; !exists {
		t.Error("Fresh scan limiter should have been kept")
	}
	scanMu.Unlock()
}

func TestRateLimitMiddlewareStaticBypass(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := RateLimitMiddleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.1:4321"

	// Exhaust the burst for this IP.
	for i := 0; i < 5; i++ {
		middleware.ServeHTTP(httptest.NewRecorder(), req)
	}
	limited := httptest.NewRecorder()
	middleware.ServeHTTP(limited, req)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected status 429 after exhausting burst, got %d", limited.Code)
	}

	// Static assets bypass the limiter even when the IP is limited.
	staticReq := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	staticReq.RemoteAddr = "198.51.100.1:4321"
	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, staticReq)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 for /static/ path, got %d", rr.Code)
	}
}

func TestProxyMiddlewareSingleForwardedFor(t *testing.T) {
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("TRUST_CLOUDFLARE", "false")

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := ProxyMiddleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", " 8.8.8.8 ")
	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)

	if req.RemoteAddr != "8.8.8.8" {
		t.Errorf("Expected RemoteAddr to be 8.8.8.8, got %s", req.RemoteAddr)
	}
}
