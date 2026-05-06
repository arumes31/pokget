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
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	password := "SecretPassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if hash == password {
		t.Error("Hash should not be equal to plain password")
	}

	if !CheckPasswordHash(password, hash) {
		t.Error("Password check should succeed with correct password")
	}

	if CheckPasswordHash("wrongpassword", hash) {
		t.Error("Password check should fail with incorrect password")
	}
}

func TestMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(UserContextKey{})
		if userID == nil {
			t.Error("User ID not found in context")
		}
		if userID.(string) != "test-user" {
			t.Errorf("Expected user ID 'test-user', got '%v'", userID)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := Middleware(nextHandler)

	t.Run("NoSession", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", rr.Code)
		}
		if rr.Header().Get("Location") != "/auth" {
			t.Errorf("Expected redirect to /auth, got %s", rr.Header().Get("Location"))
		}
	})

	t.Run("WithValidSession", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		session, _ := Store.Get(req, "session")
		session.Values["user_id"] = "test-user"
		err := session.Save(req, rr)
		if err != nil {
			t.Fatalf("Failed to save session: %v", err)
		}

		// Use the cookie from the recorder in the next request
		req.Header.Set("Cookie", rr.Header().Get("Set-Cookie"))
		rr = httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("InvalidUserIDType", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		session, _ := Store.Get(req, "session")
		session.Values["user_id"] = 123 // Should be string
		_ = session.Save(req, rr)

		req.Header.Set("Cookie", rr.Header().Get("Set-Cookie"))
		rr = httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303 for invalid user_id type, got %d", rr.Code)
		}
	})

	t.Run("WithInvalidSessionCookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "invalid-cookie-data"})
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303 for invalid session, got %d", rr.Code)
		}
	})

	t.Run("WithEmptyUserID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()

		session, _ := Store.Get(req, "session")
		session.Values["user_id"] = ""
		_ = session.Save(req, rr)

		req.Header.Set("Cookie", rr.Header().Get("Set-Cookie"))
		rr = httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if rr.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303 for empty user ID, got %d", rr.Code)
		}
	})
}

func TestHashPassword_Error(t *testing.T) {
	// bcrypt has a 72 byte limit for passwords
	longPassword := ""
	for i := 0; i < 73; i++ {
		longPassword += "a"
	}
	_, err := HashPassword(longPassword)
	if err == nil {
		t.Error("Expected error when hashing password > 72 bytes")
	}
}

func TestProxyMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("TrustCloudflare", func(t *testing.T) {
		os.Setenv("TRUST_CLOUDFLARE", "true")
		defer os.Unsetenv("TRUST_CLOUDFLARE")

		middleware := ProxyMiddleware(nextHandler)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("CF-Connecting-IP", "1.1.1.1")
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if req.RemoteAddr != "1.1.1.1" {
			t.Errorf("Expected RemoteAddr to be 1.1.1.1, got %s", req.RemoteAddr)
		}
	})

	t.Run("TrustProxy-X-Real-IP", func(t *testing.T) {
		os.Setenv("TRUST_PROXY", "true")
		os.Setenv("TRUST_CLOUDFLARE", "false")
		defer os.Unsetenv("TRUST_PROXY")
		defer os.Unsetenv("TRUST_CLOUDFLARE")

		middleware := ProxyMiddleware(nextHandler)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", "2.2.2.2")
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if req.RemoteAddr != "2.2.2.2" {
			t.Errorf("Expected RemoteAddr to be 2.2.2.2, got %s", req.RemoteAddr)
		}
	})

	t.Run("TrustProxy-X-Forwarded-For", func(t *testing.T) {
		os.Setenv("TRUST_PROXY", "true")
		os.Setenv("TRUST_CLOUDFLARE", "false")
		defer os.Unsetenv("TRUST_PROXY")
		defer os.Unsetenv("TRUST_CLOUDFLARE")

		middleware := ProxyMiddleware(nextHandler)
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "3.3.3.3, 4.4.4.4")
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if req.RemoteAddr != "3.3.3.3" {
			t.Errorf("Expected RemoteAddr to be 3.3.3.3, got %s", req.RemoteAddr)
		}
	})

	t.Run("NoTrust", func(t *testing.T) {
		os.Setenv("TRUST_PROXY", "false")
		os.Setenv("TRUST_CLOUDFLARE", "false")
		defer os.Unsetenv("TRUST_PROXY")
		defer os.Unsetenv("TRUST_CLOUDFLARE")

		middleware := ProxyMiddleware(nextHandler)
		req := httptest.NewRequest("GET", "/", nil)
		originalRemoteAddr := req.RemoteAddr
		req.Header.Set("X-Real-IP", "5.5.5.5")
		rr := httptest.NewRecorder()

		middleware.ServeHTTP(rr, req)

		if req.RemoteAddr != originalRemoteAddr {
			t.Errorf("Expected RemoteAddr to be unchanged (%s), got %s", originalRemoteAddr, req.RemoteAddr)
		}
	})
}

func TestRateLimitMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimitMiddleware(nextHandler)

	t.Run("SingleRequest", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("RateLimited", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4"
		
		// Fill the bucket (limit is 5 requests per second per IP)
		for i := 0; i < 5; i++ {
			rr := httptest.NewRecorder()
			middleware.ServeHTTP(rr, req)
		}

		// 6th request should be limited
		rr := httptest.NewRecorder()
		middleware.ServeHTTP(rr, req)
		if rr.Code != http.StatusTooManyRequests {
			t.Errorf("Expected status 429, got %d", rr.Code)
		}
	})
}
