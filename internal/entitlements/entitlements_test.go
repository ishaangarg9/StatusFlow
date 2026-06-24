package entitlements

import "testing"

func TestLimitsFor(t *testing.T) {
	free := LimitsFor(PlanFree)
	if free.MaxMonitors != 3 || free.MaxStatusPages != 1 {
		t.Fatalf("free limits drifted: %+v", free)
	}

	pro := LimitsFor(PlanPro)
	if pro.MaxMonitors != Unlimited || pro.MaxStatusPages != Unlimited {
		t.Fatalf("pro should be unlimited: %+v", pro)
	}

	// An unknown/garbled plan must resolve to the most restrictive (Free), so a
	// bad plan value can never widen entitlements.
	unknown := LimitsFor(Plan("enterprise-haxx"))
	if unknown != free {
		t.Fatalf("unknown plan must fall back to Free, got %+v", unknown)
	}
}

func TestWithinLimit(t *testing.T) {
	cases := []struct {
		current, limit int
		want           bool
	}{
		{0, 3, true},
		{2, 3, true},  // creating the 3rd is allowed
		{3, 3, false}, // the 4th is not
		{4, 3, false},
		{0, Unlimited, true},
		{9999, Unlimited, true},
	}
	for _, c := range cases {
		if got := WithinLimit(c.current, c.limit); got != c.want {
			t.Errorf("WithinLimit(%d, %d) = %v; want %v", c.current, c.limit, got, c.want)
		}
	}
}
