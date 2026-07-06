package auth

import (
	"sync"

	"github.com/alexedwards/argon2id"
)

// HashPassword returns an argon2id PHC-encoded hash. Defaults are sensible.
// Never store plaintext; never hand-roll crypto.
func HashPassword(pw string) (string, error) {
	return argon2id.CreateHash(pw, argon2id.DefaultParams)
}

// VerifyPassword constant-time compares pw against the PHC-encoded hash.
func VerifyPassword(encodedHash, pw string) (bool, error) {
	return argon2id.ComparePasswordAndHash(pw, encodedHash)
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// DummyVerify runs a full argon2id verification against a fixed hash and
// discards the result. Its only purpose is to equalise the cost of the login
// path when the email is unknown: without it, an unknown email returns right
// after the "no rows" SELECT while a known email pays for a real verify, and
// that timing gap is an account-enumeration oracle. Call it on the no-rows
// path so both paths cost one argon2 verification. The hash is computed once,
// lazily, with the same DefaultParams a real stored hash uses.
func DummyVerify(pw string) {
	dummyHashOnce.Do(func() {
		// A fixed internal input; the supplied password is compared against this
		// so the verify does real work. CreateHash can't fail with valid params,
		// so ignore the error to keep the call infallible.
		h, _ := argon2id.CreateHash("statusflow-dummy-verify-password", argon2id.DefaultParams)
		dummyHash = h
	})
	_, _ = argon2id.ComparePasswordAndHash(pw, dummyHash)
}
