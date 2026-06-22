package shared

import "regexp"

// emailRe is a deliberately simple, anchored shape check: a non-empty local
// part, an "@", and a dotted domain. It is not an RFC 5322 validator (that is
// famously not worth attempting) — it exists to reject obvious garbage at the
// edge. The anchors prevent partial matches.
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// ValidEmail reports whether s is a plausible email address. It is the single
// definition shared by signup and invitations so the two paths never disagree
// on what is acceptable. Callers are expected to trim/lowercase first.
func ValidEmail(s string) bool {
	return s != "" && len(s) <= 254 && emailRe.MatchString(s)
}

// slugRe accepts lowercase letters/digits in hyphen-separated groups — no
// leading/trailing/consecutive hyphens. It is the single definition shared by
// org slugs and status-page slugs so the two paths never disagree (both feed
// public, URL-facing identifiers).
var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidSlug reports whether s is an acceptable URL slug: 2–63 chars, lowercase
// letters, digits, and single hyphens. Callers are expected to trim/lowercase
// first.
func ValidSlug(s string) bool {
	return len(s) >= 2 && len(s) <= 63 && slugRe.MatchString(s)
}
