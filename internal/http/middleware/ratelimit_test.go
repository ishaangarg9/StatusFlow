package middleware

import (
	"net/http"
	"net/netip"
	"testing"
)

func mustPrefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			t.Fatalf("bad cidr %q: %v", c, err)
		}
		out = append(out, p)
	}
	return out
}

func reqWith(remoteAddr, xff string) *http.Request {
	r := &http.Request{Header: http.Header{}, RemoteAddr: remoteAddr}
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")

	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		trusted    []netip.Prefix
		want       string
	}{
		{
			name:       "no trusted proxies ignores XFF",
			remoteAddr: "203.0.113.7:5000",
			xff:        "1.2.3.4",
			trusted:    nil,
			want:       "203.0.113.7",
		},
		{
			name:       "untrusted peer ignores XFF (no spoofing)",
			remoteAddr: "203.0.113.7:5000",
			xff:        "1.2.3.4",
			trusted:    trusted,
			want:       "203.0.113.7",
		},
		{
			name:       "trusted peer takes rightmost untrusted hop",
			remoteAddr: "10.1.2.3:5000",
			xff:        "9.9.9.9, 198.51.100.23",
			trusted:    trusted,
			want:       "198.51.100.23",
		},
		{
			name:       "trusted peer skips trusted hops in the chain",
			remoteAddr: "10.1.2.3:5000",
			xff:        "198.51.100.23, 10.4.5.6",
			trusted:    trusted,
			want:       "198.51.100.23",
		},
		{
			name:       "spoofed leftmost XFF is not returned",
			remoteAddr: "10.1.2.3:5000",
			xff:        "127.0.0.1, 198.51.100.23",
			trusted:    trusted,
			want:       "198.51.100.23",
		},
		{
			name:       "trusted peer no XFF falls back to peer",
			remoteAddr: "10.1.2.3:5000",
			xff:        "",
			trusted:    trusted,
			want:       "10.1.2.3",
		},
		{
			name:       "all hops trusted falls back to peer",
			remoteAddr: "10.1.2.3:5000",
			xff:        "10.7.8.9, 10.4.5.6",
			trusted:    trusted,
			want:       "10.1.2.3",
		},
		{
			name:       "unparseable rightmost hop falls back to peer",
			remoteAddr: "10.1.2.3:5000",
			xff:        "198.51.100.23, unknown",
			trusted:    trusted,
			want:       "10.1.2.3",
		},
		{
			name:       "trailing empty hop falls back to peer",
			remoteAddr: "10.1.2.3:5000",
			xff:        "198.51.100.23, ",
			trusted:    trusted,
			want:       "10.1.2.3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClientIP(reqWith(tc.remoteAddr, tc.xff), tc.trusted)
			if got != tc.want {
				t.Fatalf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIPRateLimit_PerClientBucket proves that, behind a trusted proxy, two
// distinct client IPs get independent buckets (rather than collapsing into one
// shared bucket keyed on the proxy's peer address).
func TestIPRateLimit_PerClientBucket(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")
	mw := IPRateLimit(1, trusted) // 1/min, burst 1
	var served int
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served++ }))

	call := func(client string) int {
		rec := &statusRecorder{code: 200}
		h.ServeHTTP(rec, reqWith("10.0.0.9:5000", client))
		return rec.code
	}

	if got := call("198.51.100.1"); got != 200 {
		t.Fatalf("client A first call = %d, want 200", got)
	}
	if got := call("198.51.100.1"); got != 429 {
		t.Fatalf("client A second call = %d, want 429 (bucket exhausted)", got)
	}
	// A different client must NOT be throttled by client A's usage.
	if got := call("198.51.100.2"); got != 200 {
		t.Fatalf("client B first call = %d, want 200 (independent bucket)", got)
	}
	if served != 2 {
		t.Fatalf("served %d requests, want 2", served)
	}
}

type statusRecorder struct {
	code int
}

func (r *statusRecorder) Header() http.Header         { return http.Header{} }
func (r *statusRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *statusRecorder) WriteHeader(code int)        { r.code = code }
