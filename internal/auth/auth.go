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
	"net/http"
	"os"
	"sync"
	"log/slog"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
)

var Store *sessions.CookieStore

func init() {
	key := os.Getenv("SESSION_KEY")
	if key == "" {
		key = "temporary-insecure-dev-key-32-chars-long" 
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

var (
	limiters = make(map[string]*rate.Limiter)
	mu       sync.Mutex
)

func getLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	limiter, exists := limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(1, 5) // 1 request per second with a burst of 5
		limiters[ip] = limiter
	}

	return limiter
}

func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use r.RemoteAddr which is potentially updated by ProxyMiddleware
		ip := r.RemoteAddr
		limiter := getLimiter(ip)
		if !limiter.Allow() {
			slog.Warn("auth: rate limit exceeded", "ip", ip)
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
