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
	"container/list"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Login-lockout semantics
// -----------------------
// Failed logins are counted along TWO dimensions, both scoped to the client IP
// (as resolved by ProxyMiddleware / ClientIP), never to the account alone:
//
//  1. Per (IP, account) pair — the primary hard lock. After maxLoginFailures
//     failures for the same account from the same IP within loginFailureWindow,
//     that pair is blocked for the rest of the window. Because the lock is keyed
//     on the attacker's own IP, a third party can NEVER lock a victim out of
//     their own IP: the victim's (their-IP, account) pair is independent. This
//     removes the account-enumeration/DoS that a pure per-email lock allowed.
//
//  2. Per IP (aggregate across all accounts) — a secondary, higher hard lock
//     that stops credential stuffing (one IP spraying one bad password across
//     many accounts), which the per-pair lock alone would miss. Its key space is
//     bounded by the number of source IPs an attacker actually controls.
//
// The per-account dimension therefore acts only as a softer signal: it tightens
// the per-pair budget but never denies a login on its own. A successful login
// clears only the (IP, account) pair, never the per-IP aggregate, so an attacker
// who happens to own one valid account cannot wipe their own spray counter.
//
// Saturation fails CLOSED: each counter map is bounded by maxTrackedLogins and,
// when full, evicts the entry closest to expiry (FIFO by window start) instead
// of silently refusing to track new keys. Tracking never turns itself off, so an
// attacker cannot bypass the limiter by flooding it with distinct keys.
//
// Deployment note: when the app runs behind a reverse proxy but TRUST_PROXY is
// false, every request appears to come from the proxy's single IP, which
// collapses both dimensions onto one bucket. Set TRUST_PROXY/TRUSTED_PROXIES so
// the real client IP is used; otherwise legitimate users behind the proxy share
// one lockout budget.
const (
	// loginFailureWindow is the rolling window over which failures accumulate.
	loginFailureWindow = 15 * time.Minute

	// maxLoginFailures hard-locks a single (IP, account) pair for the window.
	maxLoginFailures = 10

	// maxIPLoginFailures hard-locks a client IP across all accounts once it has
	// produced this many failures within the window (credential stuffing). It is
	// set well above the per-pair limit so ordinary shared-NAT users are not
	// caught, while a single IP making this many failed logins is clearly hostile.
	maxIPLoginFailures = 100

	// maxTrackedLogins bounds each counter map so memory stays bounded under a
	// spray of distinct keys. On saturation the entry closest to expiry is
	// evicted, so the limiter keeps limiting (fails closed) rather than dropping
	// new keys untracked (which would fail open).
	maxTrackedLogins = 100000
)

// loginFailureEntry counts failures for one key within a single window.
type loginFailureEntry struct {
	failures    int
	windowStart time.Time
	elem        *list.Element // position in the owning tracker's FIFO order
}

// failureTracker is a bounded, window-based failure counter. Entries are held in
// FIFO order of their window start (front = oldest = closest to expiry), so both
// capacity eviction and stale eviction are O(1) per removed entry and never scan
// the whole map under the lock.
type failureTracker struct {
	entries  map[string]*loginFailureEntry
	order    *list.List // each element's Value is the key string
	limit    int
	capacity int
	window   time.Duration
}

func newFailureTracker(limit, capacity int, window time.Duration) *failureTracker {
	return &failureTracker{
		entries:  make(map[string]*loginFailureEntry),
		order:    list.New(),
		limit:    limit,
		capacity: capacity,
		window:   window,
	}
}

// locked reports whether key is at or above the limit within an unexpired window.
func (t *failureTracker) locked(key string, now time.Time) bool {
	entry, ok := t.entries[key]
	if !ok || now.Sub(entry.windowStart) > t.window {
		return false
	}
	return entry.failures >= t.limit
}

// record counts one failure for key, starting or continuing its window.
func (t *failureTracker) record(key string, now time.Time) {
	if entry, ok := t.entries[key]; ok {
		if now.Sub(entry.windowStart) > t.window {
			// Window expired: start a fresh one and move to the back (newest),
			// keeping the list ordered by window start.
			entry.failures = 1
			entry.windowStart = now
			t.order.MoveToBack(entry.elem)
		} else {
			entry.failures++
		}
		return
	}
	// New key. Evict the entry closest to expiry if at capacity so tracking
	// cannot silently stop under saturation.
	if t.capacity > 0 && len(t.entries) >= t.capacity {
		if front := t.order.Front(); front != nil {
			t.order.Remove(front)
			delete(t.entries, front.Value.(string))
		}
	}
	entry := &loginFailureEntry{failures: 1, windowStart: now}
	entry.elem = t.order.PushBack(key)
	t.entries[key] = entry
}

// reset removes any counter for key.
func (t *failureTracker) reset(key string) {
	if entry, ok := t.entries[key]; ok {
		t.order.Remove(entry.elem)
		delete(t.entries, key)
	}
}

// evictExpired removes every entry whose window has ended. Expired entries
// cluster at the front because the list is ordered by window start.
func (t *failureTracker) evictExpired(now time.Time) {
	for {
		front := t.order.Front()
		if front == nil {
			return
		}
		entry := t.entries[front.Value.(string)]
		if entry != nil && now.Sub(entry.windowStart) <= t.window {
			return // front is the oldest; nothing after it can be expired
		}
		t.order.Remove(front)
		if entry != nil {
			delete(t.entries, front.Value.(string))
		}
	}
}

// loginLimiter combines the per-(IP, account) and per-IP failure trackers behind
// one mutex, with an injectable clock for deterministic tests.
type loginLimiter struct {
	mu   sync.Mutex
	pair *failureTracker
	ip   *failureTracker
	now  func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		pair: newFailureTracker(maxLoginFailures, maxTrackedLogins, loginFailureWindow),
		ip:   newFailureTracker(maxIPLoginFailures, maxTrackedLogins, loginFailureWindow),
		now:  time.Now,
	}
}

var defaultLoginLimiter = newLoginLimiter()

// loginFailureKey canonicalizes the account identifier the same way the handlers
// canonicalize stored emails, so case and surrounding space never create a
// second bucket for one account.
func loginFailureKey(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// pairKey combines the client IP and canonical account into one tracker key.
// NUL cannot appear in either part, so it is a safe separator.
func pairKey(ip, email string) string {
	return ip + "\x00" + loginFailureKey(email)
}

func (l *loginLimiter) allowed(email, ip string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if ip != "" && l.ip.locked(ip, now) {
		return false
	}
	return !l.pair.locked(pairKey(ip, email), now)
}

func (l *loginLimiter) record(email, ip string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pair.record(pairKey(ip, email), now)
	if ip != "" {
		l.ip.record(ip, now)
	}
}

func (l *loginLimiter) reset(email, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Clear only the (IP, account) pair; keep the per-IP aggregate (see the
	// package comment above).
	l.pair.reset(pairKey(ip, email))
}

func (l *loginLimiter) evictExpired(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pair.evictExpired(now)
	l.ip.evictExpired(now)
}

// LoginAllowed reports whether a login attempt for the given account from the
// given client IP is currently permitted by the lockout policy.
func LoginAllowed(email, ip string) bool {
	return defaultLoginLimiter.allowed(email, ip)
}

// RecordLoginFailure counts a failed login for the account and client IP,
// whether or not the account exists, so the limiter does not reveal which
// emails are registered.
func RecordLoginFailure(email, ip string) {
	defaultLoginLimiter.record(email, ip)
}

// ResetLoginFailures clears the (IP, account) pair counter after a successful
// login. The per-IP aggregate is intentionally preserved.
func ResetLoginFailures(email, ip string) {
	defaultLoginLimiter.reset(email, ip)
}

// resetLoginLimiterForTest replaces the process-global login limiter with a
// fresh one. Tests in this package share that global state; they call this to
// isolate from each other.
func resetLoginLimiterForTest() {
	defaultLoginLimiter = newLoginLimiter()
}

// ClientIP returns the lockout/rate-limit identity for a request: the peer IP
// (after ProxyMiddleware has resolved any trusted forwarding headers), grouped
// by IPv6 /64 the same way the HTTP rate limiter keys, so a single subscriber
// cannot rotate addresses to dodge the lockout.
func ClientIP(r *http.Request) string {
	return limiterKey(clientIP(r))
}
