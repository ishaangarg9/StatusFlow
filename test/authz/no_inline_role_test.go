package authz_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// roleComparison matches a role constant (bare or authz-qualified) used with an
// equality/inequality operator on the same line — i.e. an inline role check.
// Examples it catches: `ac.Role == RoleOwner`, `r != authz.RoleAdmin`.
var roleComparison = regexp.MustCompile(`(?:==|!=)\s*(?:authz\.)?Role(?:Owner|Admin|Member|Viewer)\b|(?:authz\.)?Role(?:Owner|Admin|Member|Viewer)\b\s*(?:==|!=)`)

// TestNoInlineRoleChecks enforces CLAUDE.md §2.5 / doc 05 §3 / doc 07 §9.3:
// "Never write `if role == …` outside internal/authz/. CI greps for this and
// fails the build." All permission decisions must funnel through authz.Can.
//
// It scans cmd/ and internal/ (excluding internal/authz/ itself and _test.go
// files) and fails if any line compares against a role constant.
func TestNoInlineRoleChecks(t *testing.T) {
	root := filepath.Join("..", "..") // test/authz -> repo root
	var offenders []string

	for _, dir := range []string{"cmd", "internal"} {
		base := filepath.Join(root, dir)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// The authz package is the one place role logic is allowed to live.
			if strings.Contains(filepath.ToSlash(path), "/internal/authz/") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(src), "\n") {
				code := line
				if idx := strings.Index(code, "//"); idx >= 0 {
					code = code[:idx] // ignore comments
				}
				if roleComparison.MatchString(code) {
					offenders = append(offenders, filepath.ToSlash(path)+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", base, err)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("inline role comparison(s) found outside internal/authz/ — route the decision through authz.Can instead:\n%s",
			strings.Join(offenders, "\n"))
	}
}
