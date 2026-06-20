package worker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Pinger runs an HTTP check against a monitor's URL. The dialer is
// SSRF-guarded: it refuses to connect to loopback, RFC-1918 private,
// link-local, or cloud-metadata IPs. The check happens at *dial time* —
// AFTER name resolution — which defeats DNS rebinding (an attacker
// cannot trick the worker by serving an A record that points at 127.0.0.1).
type Pinger struct {
	client *http.Client
}

// CheckResult is the outcome the worker persists.
type CheckResult struct {
	Status     string // "up" | "down"
	StatusCode int
	LatencyMs  int
	Error      string
}

// metadataIPs lists known cloud-metadata endpoints to deny regardless of
// network classification.
var metadataIPs = []net.IP{
	net.ParseIP("169.254.169.254"), // AWS / Azure / GCE
	net.ParseIP("fd00:ec2::254"),   // AWS IMDSv6
}

func NewPinger() *Pinger {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		// Control runs AFTER name resolution and BEFORE connect. The address
		// arg is "ip:port" so we have the resolved IP in hand.
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return errors.New("ssrf: unresolved host")
			}
			if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() ||
				ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
				ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
				return errors.New("ssrf: blocked destination (" + ip.String() + ")")
			}
			for _, mip := range metadataIPs {
				if mip != nil && ip.Equal(mip) {
					return errors.New("ssrf: cloud-metadata IP")
				}
			}
			return nil
		},
	}
	return &Pinger{
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:           dialer.DialContext,
				DisableKeepAlives:     true,
				ResponseHeaderTimeout: 15 * time.Second,
				MaxIdleConns:          0,
			},
			// Don't follow redirects: each hop must re-validate via the SSRF dialer,
			// and silently chasing redirects masks where the failure happened.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Check runs one HTTP check and returns the resulting CheckResult.
// The caller is expected to wrap with an outer context.WithTimeout matching
// the monitor's timeout_ms; the http.Client honours that deadline.
func (p *Pinger) Check(ctx context.Context, m Monitor) CheckResult {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, m.Method, m.URL, nil)
	if err != nil {
		return CheckResult{Status: "down", Error: err.Error(), LatencyMs: int(time.Since(start).Milliseconds())}
	}
	res, err := p.client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return CheckResult{Status: "down", Error: err.Error(), LatencyMs: latency}
	}
	defer res.Body.Close()

	r := CheckResult{Status: "up", StatusCode: res.StatusCode, LatencyMs: latency}
	if res.StatusCode != m.ExpectedStatus {
		r.Status = "down"
		r.Error = "unexpected status"
	}
	return r
}

// runCheck is wired by scheduler.go; see TODO inside.
func (w *Worker) runCheck(ctx context.Context, m Monitor) {
	cctx, cancel := context.WithTimeout(ctx, time.Duration(m.TimeoutMs)*time.Millisecond)
	defer cancel()

	res := w.pinger.Check(cctx, m)
	_ = res
	// TODO(scaffold): inside tenancy.WithOrgTx(ctx, w.pool, m.OrgID, ...):
	//   INSERT INTO check_results (...);
	//   UPDATE monitors SET next_check_at = now() + interval_seconds * '1 second' WHERE id = m.ID;
	//   w.incidents.Apply(ctx, tx, m.OrgID, m.ID, res.Status);
}
