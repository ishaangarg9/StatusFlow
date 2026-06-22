package auth

import (
	"testing"
	"time"
)

// fakeClock lets the test advance time deterministically instead of sleeping.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestThrottle(max int, window time.Duration) (*LoginThrottle, *fakeClock) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	th := NewLoginThrottle(max, window)
	th.now = clk.now
	return th, clk
}

func TestLoginThrottle_BlocksAfterMaxFailures(t *testing.T) {
	th, _ := newTestThrottle(3, time.Minute)
	const key = "victim@example.com"

	for i := range 3 {
		if !th.Allowed(key) {
			t.Fatalf("attempt %d should be allowed (budget not yet spent)", i+1)
		}
		th.Fail(key)
	}
	if th.Allowed(key) {
		t.Fatal("expected throttle to block after max failures")
	}
}

func TestLoginThrottle_ResetClearsBudget(t *testing.T) {
	th, _ := newTestThrottle(2, time.Minute)
	const key = "user@example.com"

	th.Fail(key)
	th.Fail(key)
	if th.Allowed(key) {
		t.Fatal("expected block after 2 failures")
	}
	// A successful login resets the counter — the user is not locked out.
	th.Reset(key)
	if !th.Allowed(key) {
		t.Fatal("expected Reset to clear the counter")
	}
}

func TestLoginThrottle_WindowExpiry(t *testing.T) {
	th, clk := newTestThrottle(2, time.Minute)
	const key = "user@example.com"

	th.Fail(key)
	th.Fail(key)
	if th.Allowed(key) {
		t.Fatal("expected block within the window")
	}
	clk.add(time.Minute) // window fully elapsed
	if !th.Allowed(key) {
		t.Fatal("expected the counter to reset once the window elapsed")
	}
}

func TestLoginThrottle_SweepsExpiredKeys(t *testing.T) {
	th, clk := newTestThrottle(3, time.Minute)

	// Spray many distinct keys (mimics credential stuffing over unknown emails).
	for i := range 1000 {
		th.Fail("user" + string(rune('a'+i%26)) + "-" + time.Duration(i).String())
	}
	if th.Len() == 0 {
		t.Fatal("expected keys to be tracked")
	}
	// After a full window, the next touch must sweep all stale entries so the map
	// doesn't grow without bound.
	clk.add(time.Minute + time.Second)
	th.Fail("trigger-sweep")
	if n := th.Len(); n != 1 {
		t.Fatalf("expected the sweep to drop all expired keys (only the new one left), got %d", n)
	}
}

func TestLoginThrottle_IndependentKeys(t *testing.T) {
	th, _ := newTestThrottle(1, time.Minute)

	th.Fail("a@example.com")
	if th.Allowed("a@example.com") {
		t.Fatal("a should be blocked")
	}
	if !th.Allowed("b@example.com") {
		t.Fatal("b must not be affected by a's failures")
	}
}
