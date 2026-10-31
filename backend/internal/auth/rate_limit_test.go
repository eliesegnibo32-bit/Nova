// Unit tests for the login rate limiter.
package auth

import (
	"testing"
	"time"
)

func TestRateLimiterLocksAfterMaxAttempts(t *testing.T) {
	rl := NewLoginRateLimiter(3, 50*time.Millisecond)
	defer rl.Stop()
	id := "alice@nova.ci|10.0.0.1"

	// 3 failures should lock.
	for i := 0; i < 3; i++ {
		if rl.IsLocked(id) {
			t.Fatalf("locked after only %d failures (expected %d)", i, 3)
		}
		rl.RecordFailure(id)
	}
	if !rl.IsLocked(id) {
		t.Fatalf("expected locked after 3 failures")
	}
	if rl.RemainingAttempts(id) != 0 {
		t.Fatalf("expected 0 remaining attempts, got %d", rl.RemainingAttempts(id))
	}
}

func TestRateLimiterUnlocksAfterDuration(t *testing.T) {
	rl := NewLoginRateLimiter(2, 30*time.Millisecond)
	defer rl.Stop()
	id := "bob@nova.ci|10.0.0.2"

	rl.RecordFailure(id)
	rl.RecordFailure(id)
	if !rl.IsLocked(id) {
		t.Fatalf("expected locked after 2 failures")
	}

	// Wait for the lock to expire.
	time.Sleep(40 * time.Millisecond)
	if rl.IsLocked(id) {
		t.Fatalf("expected unlocked after lockDuration")
	}

	// After expiry, a single failure should not re-lock immediately —
	// the counter resets on the next failure post-expiry.
	rl.RecordFailure(id)
	if rl.IsLocked(id) {
		t.Fatalf("expected not locked after 1 post-expiry failure")
	}
}

func TestRateLimiterSuccessResets(t *testing.T) {
	rl := NewLoginRateLimiter(3, time.Minute)
	defer rl.Stop()
	id := "carol@nova.ci|10.0.0.3"

	rl.RecordFailure(id)
	rl.RecordFailure(id)
	rl.RecordSuccess(id)
	if rl.RemainingAttempts(id) != 3 {
		t.Fatalf("expected 3 remaining after success, got %d", rl.RemainingAttempts(id))
	}
	if rl.IsLocked(id) {
		t.Fatalf("expected not locked after success")
	}
}

func TestRateLimiterDistinctIdentifiers(t *testing.T) {
	rl := NewLoginRateLimiter(2, time.Minute)
	defer rl.Stop()
	// Same email, different IP — distinct identifiers.
	rl.RecordFailure("dave@nova.ci|1.1.1.1")
	rl.RecordFailure("dave@nova.ci|1.1.1.1")
	if !rl.IsLocked("dave@nova.ci|1.1.1.1") {
		t.Fatalf("expected dave@1.1.1.1 locked")
	}
	if rl.IsLocked("dave@nova.ci|2.2.2.2") {
		t.Fatalf("expected dave@2.2.2.2 NOT locked (different IP)")
	}
}

func TestLoginIdentifierNormalization(t *testing.T) {
	a := LoginIdentifier("Alice@Nova.CI", " 10.0.0.1 ")
	b := LoginIdentifier("alice@nova.ci", "10.0.0.1")
	if a != b {
		t.Fatalf("expected normalized identifiers to match: %q vs %q", a, b)
	}
}

func TestRateLimiterRetryAfter(t *testing.T) {
	rl := NewLoginRateLimiter(1, 100*time.Millisecond)
	defer rl.Stop()
	id := "eve@nova.ci|10.0.0.4"
	rl.RecordFailure(id) // immediately locks (maxAttempts=1)
	if d := rl.RetryAfter(id); d <= 0 || d > 100*time.Millisecond {
		t.Fatalf("expected retry after in (0, 100ms], got %v", d)
	}
	// Unknown identifier → 0.
	if d := rl.RetryAfter("nobody@nova.ci|10.0.0.9"); d != 0 {
		t.Fatalf("expected 0 retry for unknown id, got %v", d)
	}
}
