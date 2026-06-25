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
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

var Store *sessions.CookieStore

func init() {
	key := os.Getenv("SESSION_KEY")
	if key == "" {
		slog.Warn("SESSION_KEY not set, generating a random 32-byte key for this session. Sessions will be invalidated on restart!")
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic("Failed to generate secure session key: " + err.Error())
		}
		key = hex.EncodeToString(b)
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

// BUG-H08 FIX: Track last access time for each rate limiter entry
// so stale entries can be cleaned up periodically.
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	limiters = make(map[string]*rateLimiterEntry)
	mu       sync.Mutex
)

// cleanupInterval controls how often the background cleanup runs.
const cleanupInterval = 10 * time.Minute

// maxLimiterAge controls how long an entry can be idle before eviction.
const maxLimiterAge = 1 * time.Hour

func init() {
	// BUG-H08 FIX: Start background goroutine to periodically clean up
	// old rate limiter entries, preventing unbounded memory growth.
	go cleanupStaleLimiters()
}

// cleanupStaleLimiters removes entries that haven't been used recently.
func cleanupStaleLimiters() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		mu.Lock()
		now := time.Now()
		for ip, entry := range limiters {
			if now.Sub(entry.lastSeen) > maxLimiterAge {
				delete(limiters, ip)
			}
		}
		mu.Unlock()
	}
}

func getLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	entry, exists := limiters[ip]
	if !exists {
		rateLimit := 1.0
		burstLimit := 5

		if val := os.Getenv("RATE_LIMIT"); val != "" {
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				rateLimit = f
			}
		}
		if val := os.Getenv("BURST_LIMIT"); val != "" {
			if i, err := strconv.Atoi(val); err == nil {
				burstLimit = i
			}
		}

		limiter := rate.NewLimiter(rate.Limit(rateLimit), burstLimit)
		limiters[ip] = &rateLimiterEntry{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	entry.lastSeen = time.Now()
	return entry.limiter
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
