package auth

import (
	"sync"
	"time"
)

// LoginThrottle blunts online password guessing by limiting *failed* login
// attempts per account within a rolling window. It is in-process (per API
// instance); behind multiple instances you'd back this with Redis (ADR — ask
// before adding the dependency). It sits behind the per-IP limiter on /login,
// not in place of it: the IP limiter caps request volume from one source, this
// caps failures against one account regardless of source (credential stuffing
// rotates IPs).
//
// Tradeoff: keying on the account lets someone who knows a victim's email burn
// their failure budget as a denial of service. We accept that only as a *soft*
// throttle — a generous budget over a short window, never a long hard lockout —
// so a real user retries successfully within a minute while a guessing loop is
// throttled to a crawl. A successful login clears the counter immediately.
type LoginThrottle struct {
	max    int
	window time.Duration

	mu        sync.Mutex
	hits      map[string]*failCounter
	nextSweep time.Time
	now       func() time.Time // injectable for tests
}

type failCounter struct {
	count       int
	windowStart time.Time
}

// NewLoginThrottle allows up to max failed attempts per key per window.
func NewLoginThrottle(max int, window time.Duration) *LoginThrottle {
	if max <= 0 {
		max = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	return &LoginThrottle{
		max:    max,
		window: window,
		hits:   map[string]*failCounter{},
		now:    time.Now,
	}
}

// sweepLocked drops every counter whose window has fully elapsed. The key space
// is attacker-controlled (Fail is called for unknown emails too), so without
// this the map would grow without bound under a spray of distinct keys that are
// never revisited — turning the anti-DoS throttle into a memory-DoS. We sweep at
// most once per window so the O(n) scan is amortized to nothing. Caller holds mu.
func (t *LoginThrottle) sweepLocked(now time.Time) {
	if now.Before(t.nextSweep) {
		return
	}
	for k, fc := range t.hits {
		if now.Sub(fc.windowStart) >= t.window {
			delete(t.hits, k)
		}
	}
	t.nextSweep = now.Add(t.window)
}

// Allowed reports whether another attempt is permitted for key right now. A
// window that has fully elapsed resets the counter lazily.
func (t *LoginThrottle) Allowed(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.sweepLocked(now)
	fc := t.hits[key]
	if fc == nil {
		return true
	}
	if now.Sub(fc.windowStart) >= t.window {
		delete(t.hits, key)
		return true
	}
	return fc.count < t.max
}

// Fail records one failed attempt for key, starting a fresh window if the prior
// one elapsed.
func (t *LoginThrottle) Fail(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.sweepLocked(now)
	fc := t.hits[key]
	if fc == nil || now.Sub(fc.windowStart) >= t.window {
		t.hits[key] = &failCounter{count: 1, windowStart: now}
		return
	}
	fc.count++
}

// Len reports the number of tracked keys (for tests/observability).
func (t *LoginThrottle) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.hits)
}

// Reset clears the counter for key. Call it on a successful login so a user who
// finally types the right password is not penalised for earlier typos.
func (t *LoginThrottle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.hits, key)
}
