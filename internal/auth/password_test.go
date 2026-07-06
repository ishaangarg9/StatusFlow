package auth

import (
	"testing"
	"time"
)

// TestDummyVerify_CostsARealVerify proves the unknown-email login path (which
// calls DummyVerify) pays roughly the same argon2 cost as a real verify, so
// response time can't be used to enumerate accounts. We assert the dummy verify
// is within the same order of magnitude as a genuine verify — not instant.
func TestDummyVerify_CostsARealVerify(t *testing.T) {
	hash, err := HashPassword("some-real-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	realStart := time.Now()
	if _, err := VerifyPassword(hash, "wrong-password"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	realDur := time.Since(realStart)

	// Warm the lazy dummy hash so the measured call is verification only.
	DummyVerify("warmup")

	dummyStart := time.Now()
	DummyVerify("attacker-guess")
	dummyDur := time.Since(dummyStart)

	// A no-op would be sub-microsecond; a real argon2id verify is milliseconds.
	if dummyDur < realDur/4 {
		t.Fatalf("DummyVerify too cheap (%v) vs real verify (%v): timing oracle remains", dummyDur, realDur)
	}
}
