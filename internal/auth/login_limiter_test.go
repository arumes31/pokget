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
	ResetLoginLimiter()
	t.Cleanup(ResetLoginLimiter)

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
	ResetLoginLimiter()
	t.Cleanup(ResetLoginLimiter)

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
	ResetLoginLimiter()
	t.Cleanup(ResetLoginLimiter)

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

// An in-window lock must survive saturation: a flood of distinct keys must never
// evict an active lock (that would let a still-locked attacker back in).
func TestSaturationNeverReleasesAnActiveLock(t *testing.T) {
	now := time.Now()
	tracker := newFailureTracker(3, 2, time.Minute) // limit 3, capacity 2

	for range 3 { // lock "victim"
		tracker.record("victim", now)
	}
	if !tracker.locked("victim", now) {
		t.Fatal("victim should be locked")
	}

	// Flood far more distinct one-failure keys than the capacity.
	for i := 0; i < 50; i++ {
		tracker.record(fmt.Sprintf("flood-%d", i), now)
	}
	if !tracker.locked("victim", now) {
		t.Fatal("an active lock must survive a flood of distinct keys")
	}
}

// After a garbage flood of unlocked keys, a genuine attacker key must still be
// able to reach a lock: unlocked garbage is evicted to make room, so tracking
// does not silently turn off (fail closed).
func TestSaturationStillLocksNewAttackerAfterGarbageFlood(t *testing.T) {
	now := time.Now()
	tracker := newFailureTracker(3, 3, time.Minute)

	for i := 0; i < 3; i++ { // fill capacity with one-failure garbage
		tracker.record(fmt.Sprintf("garbage-%d", i), now)
	}
	for range 3 { // a new attacker hammers one key
		tracker.record("attacker", now)
	}
	if !tracker.locked("attacker", now) {
		t.Fatal("a new attacker key must still reach a lock despite a garbage flood")
	}
}

// When every entry is an in-window lock the new key is dropped rather than
// evicting a lock; the map stays bounded and the locks remain.
func TestSaturationDropsNewKeyWhenAllLocked(t *testing.T) {
	now := time.Now()
	tracker := newFailureTracker(3, 2, time.Minute)

	for range 3 {
		tracker.record("a", now)
	}
	for range 3 {
		tracker.record("b", now)
	}
	if !tracker.locked("a", now) || !tracker.locked("b", now) {
		t.Fatal("both keys should be locked")
	}
	tracker.record("c", now) // all entries locked -> dropped
	if _, ok := tracker.entries["c"]; ok {
		t.Error("a new key must not evict an active lock; it is dropped instead")
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
