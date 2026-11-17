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
	"fmt"
	"testing"
	"time"
)

// A third party must not be able to permanently lock a victim out of their own
// IP by submitting bad logins for the victim's email from attacker-chosen IPs.
func TestVictimNotLockableByThirdPartyFromDifferentIPs(t *testing.T) {
	resetLoginLimiterForTest()
	t.Cleanup(resetLoginLimiterForTest)

	const victim = "victim@example.com"

	// Attacker sprays the victim's email from many distinct IPs, each one well
	// past the per-pair limit.
	for i := 0; i < 50; i++ {
		attackerIP := fmt.Sprintf("198.51.100.%d", i)
		for range maxLoginFailures + 3 {
			RecordLoginFailure(victim, attackerIP)
		}
		if LoginAllowed(victim, attackerIP) {
			t.Fatalf("attacker IP %s should be locked for the victim pair", attackerIP)
		}
	}

	// The victim's own IP recorded no failures, so the victim can still log in.
	if !LoginAllowed(victim, "203.0.113.200") {
		t.Fatal("victim was locked out from their own IP by an attacker-chosen email")
	}
}

// A single IP that sprays one bad password across many accounts must be stopped
// by the per-IP aggregate lock (credential stuffing), without affecting other
// IPs.
func TestSprayingIPLockedByAggregate(t *testing.T) {
	resetLoginLimiterForTest()
	t.Cleanup(resetLoginLimiterForTest)

	const attackerIP = "198.51.100.9"
	for i := 0; i < maxIPLoginFailures; i++ {
		RecordLoginFailure(fmt.Sprintf("user%d@example.com", i), attackerIP)
	}

	// A brand-new account from that IP is blocked by the aggregate even though
	// its own pair has no failures.
	if LoginAllowed("fresh-victim@example.com", attackerIP) {
		t.Fatal("per-IP aggregate did not lock a spraying IP")
	}
	// A clean IP is unaffected.
	if !LoginAllowed("fresh-victim@example.com", "203.0.113.7") {
		t.Fatal("a clean IP must not be affected by another IP's spray")
	}
}

// A successful login clears only the (IP, account) pair, never the per-IP
// aggregate, so an attacker who owns one valid account cannot wipe their spray
// counter.
func TestResetKeepsPerIPAggregate(t *testing.T) {
	resetLoginLimiterForTest()
	t.Cleanup(resetLoginLimiterForTest)

	const ip = "198.51.100.42"
	for i := 0; i < maxIPLoginFailures; i++ {
		RecordLoginFailure(fmt.Sprintf("user%d@example.com", i), ip)
	}
	// The attacker "succeeds" on one account they control and resets its pair.
	ResetLoginFailures("user0@example.com", ip)

	// The per-IP aggregate is still saturated, so the IP stays locked.
	if LoginAllowed("brand-new@example.com", ip) {
		t.Fatal("resetting one pair must not clear the per-IP aggregate lock")
	}
}

// Under map saturation the limiter must keep tracking new keys (fail closed) by
// evicting the entry closest to expiry, rather than silently dropping new keys
// (the old fail-open bug an attacker could exploit by spraying distinct keys).
func TestFailureTrackerFailsClosedUnderSaturation(t *testing.T) {
	now := time.Now()
	tracker := newFailureTracker(3, 2, time.Minute) // limit 3, capacity 2

	for range 3 {
		tracker.record("a", now)
	}
	for range 3 {
		tracker.record("b", now)
	}
	if !tracker.locked("a", now) || !tracker.locked("b", now) {
		t.Fatal("keys that reached the limit should be locked")
	}

	// A third distinct key arrives while the map is full. The oldest entry is
	// evicted, but the new key is still tracked and still reaches the limit.
	for range 3 {
		tracker.record("c", now)
	}
	if !tracker.locked("c", now) {
		t.Fatal("new key under saturation must still be tracked and lockable (fail closed)")
	}
	if len(tracker.entries) != 2 {
		t.Fatalf("capacity exceeded: %d entries, want 2", len(tracker.entries))
	}
}

// evictExpired must drop entries whose window has ended and keep current ones.
func TestFailureTrackerEvictsExpired(t *testing.T) {
	start := time.Now()
	tracker := newFailureTracker(3, 10, time.Minute)

	tracker.record("old", start)
	tracker.record("new", start.Add(90*time.Second)) // "old" window has now ended

	tracker.evictExpired(start.Add(90 * time.Second))

	if _, ok := tracker.entries["old"]; ok {
		t.Error("expired entry should have been evicted")
	}
	if _, ok := tracker.entries["new"]; !ok {
		t.Error("current entry should have been kept")
	}
}
