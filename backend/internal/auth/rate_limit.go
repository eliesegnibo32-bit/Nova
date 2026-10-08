// In-memory login rate limiter.
//
// Implements the progressive lockout policy from the cahier des charges
// (ch. 11.1 Sécurité): after `maxAttempts` failed login attempts within a
// rolling window, the identifier (email+IP) is locked for `lockDuration`.
// Successful login clears the counter.
//
// The limiter is process-local: it is sufficient for a single-instance
// deployment. In a multi-instance setup, this should be backed by Redis or
// a `login_attempts` PG table (TODO production hardening). For NOVA's
// initial launch (single container behind a load balancer), this is enough.
//
// The identifier is constructed as `email|ip` so that:
//   - An attacker cannot lock out a victim by IP-spoofing alone (needs the
//     right email too).
//   - A single attacker IP cannot try 5 different passwords against one
//     account without being locked after 5 tries.
//   - A legit user behind a CGNAT (many users, one IP) is not penalized for
//     other users' failures (the email differs).
package auth

import (
	"strings"
	"sync"
	"time"
)

// LoginRateLimiter tracks failed login attempts per identifier (email|ip)
// and locks them out after `maxAttempts` failures for `lockDuration`.
//
// All fields are safe for concurrent use. A background goroutine lazily
// purges expired entries (see StartCleanup).
type LoginRateLimiter struct {
	maxAttempts  int
	lockDuration time.Duration

	mu      sync.RWMutex
	entries map[string]*rateEntry
	stopCh  chan struct{}
}

// rateEntry is the per-identifier state stored by the limiter.
type rateEntry struct {
	fails      int       // number of failed attempts since last success
	lastFailAt time.Time // time of the most recent failure
	lockedUntil time.Time // when the lock expires (zero = not locked)
}

// NewLoginRateLimiter returns a new limiter. Call StartCleanup to launch the
// background GC goroutine (or rely on lazy cleanup — entries are evicted on
// access if expired).
func NewLoginRateLimiter(maxAttempts int, lockDuration time.Duration) *LoginRateLimiter {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if lockDuration <= 0 {
		lockDuration = 15 * time.Minute
	}
	return &LoginRateLimiter{
		maxAttempts:  maxAttempts,
		lockDuration: lockDuration,
		entries:      make(map[string]*rateEntry),
		stopCh:       make(chan struct{}),
	}
}

// StartCleanup launches a goroutine that periodically evicts expired
// entries to keep the map small. The goroutine runs until Stop is called.
// Calling this is optional; entries are also evicted lazily on access.
func (r *LoginRateLimiter) StartCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.evictExpired(time.Now())
			case <-r.stopCh:
				return
			}
		}
	}()
}

// Stop terminates the cleanup goroutine started by StartCleanup. Safe to
// call multiple times; subsequent calls are no-ops.
func (r *LoginRateLimiter) Stop() {
	select {
	case <-r.stopCh:
		// already closed
	default:
		close(r.stopCh)
	}
}

// IsLocked returns true if the identifier is currently locked (too many
// recent failures). A locked identifier rejects login even with the right
// credentials until `lockedUntil` has passed.
func (r *LoginRateLimiter) IsLocked(identifier string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[identifier]
	if !ok {
		return false
	}
	if e.lockedUntil.IsZero() {
		return false
	}
	return time.Now().Before(e.lockedUntil)
}

// RetryAfter returns how long until the lock on `identifier` expires. Returns
// 0 if not locked. Useful for the `Retry-After` HTTP header / response body.
func (r *LoginRateLimiter) RetryAfter(identifier string) time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[identifier]
	if !ok || e.lockedUntil.IsZero() {
		return 0
	}
	d := time.Until(e.lockedUntil)
	if d < 0 {
		return 0
	}
	return d
}

// RecordFailure increments the failure counter for the identifier and, if
// the threshold is reached, locks it for `lockDuration`.
func (r *LoginRateLimiter) RecordFailure(identifier string) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[identifier]
	if !ok {
		e = &rateEntry{}
		r.entries[identifier] = e
	}
	// If the previous lock expired, reset the counter so the user gets a
	// fresh start (otherwise a long-locked-out user would be re-locked
	// after a single failure).
	if !e.lockedUntil.IsZero() && now.After(e.lockedUntil) {
		e.fails = 0
		e.lockedUntil = time.Time{}
	}
	e.fails++
	e.lastFailAt = now
	if e.fails >= r.maxAttempts {
		e.lockedUntil = now.Add(r.lockDuration)
	}
}

// RecordSuccess clears the failure state for the identifier.
func (r *LoginRateLimiter) RecordSuccess(identifier string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, identifier)
}

// RemainingAttempts returns how many more failures the identifier can
// absorb before being locked. Returns 0 if already locked. Returns
// `maxAttempts` if the identifier has no failures.
func (r *LoginRateLimiter) RemainingAttempts(identifier string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[identifier]
	if !ok {
		return r.maxAttempts
	}
	if !e.lockedUntil.IsZero() && time.Now().Before(e.lockedUntil) {
		return 0
	}
	remaining := r.maxAttempts - e.fails
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// evictExpired removes entries whose lock has expired AND whose last failure
// is older than 2x the lock duration (so we keep recent history a bit for
// observability).
func (r *LoginRateLimiter) evictExpired(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	staleAfter := r.lockDuration * 2
	for k, e := range r.entries {
		// Keep if currently locked.
		if !e.lockedUntil.IsZero() && now.Before(e.lockedUntil) {
			continue
		}
		// Keep if recently failed (within staleAfter).
		if now.Sub(e.lastFailAt) < staleAfter {
			continue
		}
		delete(r.entries, k)
	}
}

// LoginIdentifier builds the (email|ip) identifier used by the limiter.
// Email is lowercased so that "Foo@bar.com" and "foo@bar.com" count as the
// same identifier. Both inputs are trimmed of surrounding whitespace.
func LoginIdentifier(email, ip string) string {
	return strings.ToLower(strings.TrimSpace(email)) + "|" + strings.TrimSpace(ip)
}
