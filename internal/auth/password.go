package auth

import "github.com/alexedwards/argon2id"

// HashPassword returns an argon2id PHC-encoded hash. Defaults are sensible.
// Never store plaintext; never hand-roll crypto.
func HashPassword(pw string) (string, error) {
	return argon2id.CreateHash(pw, argon2id.DefaultParams)
}

// VerifyPassword constant-time compares pw against the PHC-encoded hash.
func VerifyPassword(encodedHash, pw string) (bool, error) {
	return argon2id.ComparePasswordAndHash(pw, encodedHash)
}
