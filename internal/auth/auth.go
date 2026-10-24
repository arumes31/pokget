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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	// The second key encrypts the cookie so the user ID and session version
	// are not readable by the client; the first keeps authenticating it.
	return sessions.NewCookieStore([]byte(key), sessionEncryptionKey(key))
}

// sessionEncryptionKey derives a dedicated AES-256 key for session cookies.
func sessionEncryptionKey(masterKey string) []byte {
	mac := hmac.New(sha256.New, []byte(masterKey))
	mac.Write([]byte("pokget:session:aes256"))
	return mac.Sum(nil)
}

// MinPasswordLength is the minimum number of characters for new passwords.
const MinPasswordLength = 12

// ErrPasswordTooShort reports a new password below MinPasswordLength.
var ErrPasswordTooShort = errors.New("password too short")

// ValidateNewPassword enforces the password policy for new credentials.
func ValidateNewPassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	return nil
}

// bcryptCost balances offline cracking resistance against the CPU an
// unauthenticated login or registration can consume. Hashes created with a
// different cost keep verifying because bcrypt stores the cost in the hash.
const bcryptCost = 12

// hashSlots caps concurrent bcrypt work so a burst of auth requests cannot
// occupy every core needed by scanning and page rendering.
var hashSlots = make(chan struct{}, max(1, runtime.NumCPU()/2))

func withHashSlot(work func()) {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	work()
}

func HashPassword(password string) (string, error) {
	var hash []byte
	var err error
	withHashSlot(func() {
		hash, err = bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	})
	return string(hash), err
}

func CheckPasswordHash(password, hash string) bool {
	var err error
	withHashSlot(func() {
		err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	})
	return err == nil
}

// EqualizePasswordCheck spends the same work as CheckPasswordHash for an
// unknown account.
func EqualizePasswordCheck(password string) {
	_, _ = HashPassword(password)
}

// UserContextKey is the key for the user ID in the context
type UserContextKey struct{}

var errInvalidSession = errors.New("invalid session")

func sessionVersion(value interface{}) (int64, bool) {
	switch version := value.(type) {
	case nil:
		// Cookies created before session versioning are version zero.
		return 0, true
	case int:
		return int64(version), true
	case int32:
		return int64(version), true
	case int64:
		return version, true
	default:
		return 0, false
	}
}

func authenticateRequest(database *sql.DB, r *http.Request) (string, error) {
	if database == nil {
		return "", sql.ErrConnDone
	}

	session, err := Store.Get(r, "session")
	if err != nil {
		return "", errInvalidSession
	}
	userID, ok := session.Values["user_id"].(string)
	if !ok || userID == "" {
		return "", errInvalidSession
	}
	cookieVersion, ok := sessionVersion(session.Values["session_version"])
	if !ok {
		return "", errInvalidSession
	}

	var currentVersion int64
	err = database.QueryRowContext(
		r.Context(),
		"SELECT COALESCE(session_version, 0) FROM users WHERE id = $1",
		userID,
	).Scan(&currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errInvalidSession
	}
	if err != nil {
		return "", err
	}
	if currentVersion != cookieVersion {
		return "", errInvalidSession
	}
	return userID, nil
}

func expireSession(w http.ResponseWriter, r *http.Request) {
	session, err := Store.Get(r, "session")
	if err != nil {
		return
	}
	session.Values["user_id"] = ""
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		slog.Warn("auth: failed to expire session", "error", err)
	}
}

// RevokeSession invalidates every cookie issued for the request's user by
// bumping session_version. Revoked or forged cookies are ignored.
func RevokeSession(database *sql.DB, r *http.Request) error {
	if database == nil {
		return nil
	}
	session, err := Store.Get(r, "session")
	if err != nil {
		return nil
	}
	userID, ok := session.Values["user_id"].(string)
	if !ok || userID == "" {
		return nil
	}
	cookieVersion, ok := sessionVersion(session.Values["session_version"])
	if !ok {
		return nil
	}
	_, err = database.ExecContext(
		r.Context(),
		`UPDATE users SET session_version = COALESCE(session_version, 0) + 1
		 WHERE id = $1 AND COALESCE(session_version, 0) = $2`,
		userID,
		cookieVersion,
	)
	return err
}

func Middleware(database *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := authenticateRequest(database, r)
			if errors.Is(err, errInvalidSession) {
				expireSession(w, r)
				http.Redirect(w, r, "/auth", http.StatusSeeOther)
				return
			}
			if err != nil {
				slog.Error("auth: session validation failed", "error", err)
				http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIAuthMiddleware validates the same cookie session as Middleware while
// returning API-appropriate status codes instead of HTML redirects.
func APIAuthMiddleware(database *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := authenticateRequest(database, r)
			if errors.Is(err, errInvalidSession) {
				expireSession(w, r)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if err != nil {
				slog.Error("auth: API session validation failed", "error", err)
				http.Error(w, "Authentication service unavailable", http.StatusServiceUnavailable)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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

	scanLimiters = make(map[string]*rateLimiterEntry)
	scanMu       sync.Mutex

	loginFailures   = make(map[string]*loginFailureEntry)
	loginFailuresMu sync.Mutex
)

// loginFailureEntry counts failed logins for one account within a window.
type loginFailureEntry struct {
	failures    int
	windowStart time.Time
}

const (
	// maxLoginFailures failed attempts per account lock it for the rest of
	// loginFailureWindow, independent of the client IP.
	maxLoginFailures   = 10
	loginFailureWindow = 15 * time.Minute
	// maxTrackedLogins bounds memory when attackers spray random emails.
	maxTrackedLogins = 100000
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
		evictStaleLimiters(time.Now())
	}
}

// evictStaleLimiters removes entries idle for longer than maxLimiterAge.
func evictStaleLimiters(now time.Time) {
	mu.Lock()
	for ip, entry := range limiters {
		if now.Sub(entry.lastSeen) > maxLimiterAge {
			delete(limiters, ip)
		}
	}
	mu.Unlock()

	scanMu.Lock()
	for ip, entry := range scanLimiters {
		if now.Sub(entry.lastSeen) > maxLimiterAge {
			delete(scanLimiters, ip)
		}
	}
	scanMu.Unlock()

	loginFailuresMu.Lock()
	for email, entry := range loginFailures {
		if now.Sub(entry.windowStart) > loginFailureWindow {
			delete(loginFailures, email)
		}
	}
	loginFailuresMu.Unlock()
}

func loginFailureKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// LoginAllowed reports whether the account is below the failed login limit.
func LoginAllowed(email string) bool {
	loginFailuresMu.Lock()
	defer loginFailuresMu.Unlock()

	entry, exists := loginFailures[loginFailureKey(email)]
	if !exists || time.Since(entry.windowStart) > loginFailureWindow {
		return true
	}
	return entry.failures < maxLoginFailures
}

// RecordLoginFailure counts a failed login for the account, whether or not
// it exists, so the limiter does not reveal registered emails.
func RecordLoginFailure(email string) {
	loginFailuresMu.Lock()
	defer loginFailuresMu.Unlock()

	key := loginFailureKey(email)
	entry, exists := loginFailures[key]
	if !exists || time.Since(entry.windowStart) > loginFailureWindow {
		if !exists && len(loginFailures) >= maxTrackedLogins {
			return
		}
		loginFailures[key] = &loginFailureEntry{failures: 1, windowStart: time.Now()}
		return
	}
	entry.failures++
}

// ResetLoginFailures clears the failure count after a successful login.
func ResetLoginFailures(email string) {
	loginFailuresMu.Lock()
	delete(loginFailures, loginFailureKey(email))
	loginFailuresMu.Unlock()
}

func getLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	entry, exists := limiters[ip]
	if !exists {
		rateLimit := 1.0
		burstLimit := 5

		if val := os.Getenv("RATE_LIMIT"); val != "" {
			if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
				rateLimit = f
			}
		}
		if val := os.Getenv("BURST_LIMIT"); val != "" {
			if i, err := strconv.Atoi(val); err == nil && i > 0 {
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

func getScanLimiter(ip string) *rate.Limiter {
	scanMu.Lock()
	defer scanMu.Unlock()

	entry, exists := scanLimiters[ip]
	if exists {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	requestsPerSecond := 0.2
	burst := 2
	if value := os.Getenv("SCAN_RATE_LIMIT"); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			requestsPerSecond = parsed
		}
	}
	if value := os.Getenv("SCAN_BURST_LIMIT"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			burst = parsed
		}
	}

	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
	scanLimiters[ip] = &rateLimiterEntry{limiter: limiter, lastSeen: time.Now()}
	return limiter
}

func clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

// limiterKey groups IPv6 clients by /64, since a single subscriber usually
// controls the whole prefix and could otherwise rotate addresses freely.
func limiterKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The root service-worker script is a static asset too. Browser
		// installation/update checks must not spend the navigation budget.
		serviceWorkerRead := r.URL.Path == "/sw.js" && (r.Method == http.MethodGet || r.Method == http.MethodHead)
		if strings.HasPrefix(r.URL.Path, "/static/") || serviceWorkerRead {
			next.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r)
		limiter := getLimiter(limiterKey(ip))
		if !limiter.Allow() {
			slog.Warn("auth: rate limit exceeded", "ip", ip)
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ScanRateLimitMiddleware applies a stricter, independent rate limit to the
// CPU-intensive card scanner.
func ScanRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !getScanLimiter(limiterKey(ip)).Allow() {
			slog.Warn("auth: scan rate limit exceeded", "ip", ip)
			w.Header().Set("Retry-After", "5")
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
			err := database.QueryRowContext(r.Context(), "SELECT COALESCE(is_admin, FALSE) FROM users WHERE id = $1", userID).Scan(&isAdmin)
			if err != nil || !isAdmin {
				slog.Warn("auth: non-admin user attempted admin action", "user_id", userID)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
