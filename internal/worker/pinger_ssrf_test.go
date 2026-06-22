package worker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestSSRFBlockedIP is the heart of the guard: it must reject every
// non-routable / internal address class and let public addresses through. No
// network or DB needed — this is the policy in isolation.
func TestSSRFBlockedIP(t *testing.T) {
	blocked := map[string]string{
		"loopback v4":        "127.0.0.1",
		"loopback v6":        "::1",
		"rfc1918 10/8":       "10.0.0.5",
		"rfc1918 172.16/12":  "172.16.0.1",
		"rfc1918 192.168/16": "192.168.1.1",
		"link-local v4":      "169.254.10.10",
		"link-local v6":      "fe80::1",
		"unique-local v6":    "fd00::1",
		"aws/gce metadata":   "169.254.169.254",
		"aws imdsv6":         "fd00:ec2::254",
		"multicast":          "224.0.0.1",
		"unspecified":        "0.0.0.0",
	}
	for name, s := range blocked {
		t.Run("blocked/"+name, func(t *testing.T) {
			ip := net.ParseIP(s)
			if ip == nil {
				t.Fatalf("bad test IP %q", s)
			}
			if err := ssrfBlockedIP(ip); err == nil {
				t.Fatalf("%s (%s) must be blocked, got nil", name, s)
			}
		})
	}

	allowed := map[string]string{
		"public v4":     "93.184.216.34", // example.com
		"public v4 dns": "8.8.8.8",
		"public v6":     "2606:2800:220:1:248:1893:25c8:1946",
	}
	for name, s := range allowed {
		t.Run("allowed/"+name, func(t *testing.T) {
			ip := net.ParseIP(s)
			if err := ssrfBlockedIP(ip); err != nil {
				t.Fatalf("%s (%s) must be allowed, got %v", name, s, err)
			}
		})
	}
}

// TestSSRFDialControl checks the Control hook unwraps "ip:port" and applies the
// policy, and that an unparseable host is rejected rather than allowed.
func TestSSRFDialControl(t *testing.T) {
	if err := ssrfDialControl("tcp", "127.0.0.1:80", nil); err == nil {
		t.Fatal("loopback must be rejected at the dial control")
	}
	if err := ssrfDialControl("tcp", "93.184.216.34:443", nil); err != nil {
		t.Fatalf("public IP must pass the dial control, got %v", err)
	}
	if err := ssrfDialControl("tcp", "not-an-ip:80", nil); err == nil {
		t.Fatal("unparseable host must be rejected (fail closed)")
	}
}

// TestPingerRefusesLoopback is the end-to-end proof, and the DNS-rebinding
// defense in practice: the Pinger's real http.Client is pointed at a server
// bound to 127.0.0.1 (reachable only via a loopback IP, exactly what a rebind
// would resolve to). The guard runs after resolution, so the connection is
// refused and the check comes back "down" — the server's handler never runs.
func TestPingerRefusesLoopback(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewPinger()
	res := p.Check(context.Background(), Monitor{
		ID:             uuid.New(),
		URL:            srv.URL, // http://127.0.0.1:<port>
		Method:         http.MethodGet,
		TimeoutMs:      2000,
		ExpectedStatus: 200,
	})

	if reached {
		t.Fatal("SSRF guard failed: the loopback server handler was reached")
	}
	if res.Status != "down" {
		t.Fatalf("expected status=down for a blocked loopback target, got %q", res.Status)
	}
	if !strings.Contains(res.Error, "ssrf") {
		t.Fatalf("expected an ssrf error, got %q", res.Error)
	}
}
