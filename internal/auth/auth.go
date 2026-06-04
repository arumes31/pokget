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
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

var Store *sessions.CookieStore

func init() {
	key := os.Getenv("SESSION_KEY")
	if key == "" {
		if os.Getenv("DEBUG") == "true" {
			// Generate a random key for development if not provided
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				panic(fmt.Sprintf("auth: failed to generate random session key: %v", err))
			}
			key = hex.EncodeToString(b)
			// Set it in the environment so other components (like config.Load) see it
			os.Setenv("SESSION_KEY", key)
			slog.Warn("auth: SESSION_KEY not set, using generated random key for development")
		} else {
			// Panic in production if the key is missing
			panic("auth: SESSION_KEY environment variable is required in production")
		}
	}
	Store = InitStore(key)
}

func InitStore(key string) *sessions.CookieStore {
	if len(key) < 32 {
		panic("SESSION_KEY must be at least 32 characters long")
	}
	return sessions.NewCookieStore([]byte(key))
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// UserContextKey is the key for the user ID in the context
type UserContextKey struct{}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := Store.Get(r, "session")
		if err != nil {
			// If session is invalid/tampered, clear it and redirect to auth
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}
		userID, ok := session.Values["user_id"].(string)
		if !ok || userID == "" {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), UserContextKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Optimization: Use sync.Map instead of map + mutex for rate limiters.
// This reduces lock contention on the global mutex when multiple concurrent
// requests from different IPs are being rate-limited.
// Expected Impact: Better scalability under high traffic.
var limiters sync.Map

func getLimiter(ip string) *rate.Limiter {
	if limiter, ok := limiters.Load(ip); ok {
		return limiter.(*rate.Limiter)
	}

	// Double-check with LoadOrStore to avoid race conditions during creation
	newLimiter := rate.NewLimiter(1, 5) // 1 request per second with a burst of 5
	actual, _ := limiters.LoadOrStore(ip, newLimiter)
	return actual.(*rate.Limiter)
}

func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalize r.RemoteAddr to IP-only (strip port) for consistent rate limiting
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(ip); err == nil {
			ip = host
		}
		limiter := getLimiter(ip)
		if !limiter.Allow() {
			slog.Warn("auth: rate limit exceeded", "ip", ip)
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminMiddleware restricts access to users with is_admin=true in the database.
func AdminMiddleware(database *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(UserContextKey{}).(string)
			if !ok || userID == "" {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			var isAdmin bool
			err := database.QueryRow("SELECT COALESCE(is_admin, FALSE) FROM users WHERE id = $1", userID).Scan(&isAdmin)
			if err != nil || !isAdmin {
				slog.Warn("auth: non-admin user attempted admin action", "user_id", userID)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
