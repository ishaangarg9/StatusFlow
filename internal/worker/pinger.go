package worker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ishaangarg9/statusflow/internal/tenancy"
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

// ssrfDialControl is the dialer Control hook. It runs AFTER name resolution and
// BEFORE connect, so the address arg is "ip:port" with the *resolved* IP in
// hand — which is what defeats DNS rebinding: an attacker serving an A record
// for 127.0.0.1 still gets rejected here, because we judge the resolved IP, not
// the hostname. Extracted to package scope so it is unit-testable.
func ssrfDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("ssrf: unresolved host")
	}
	return ssrfBlockedIP(ip)
}

// ssrfBlockedIP returns a non-nil error if ip is one the worker must never
// connect to: non-global-unicast, loopback, RFC-1918/ULA private, link-local
// (incl. 169.254.0.0/16, which covers IMDS), any multicast, or a known
// cloud-metadata address.
func ssrfBlockedIP(ip net.IP) error {
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
}

func NewPinger() *Pinger {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   ssrfDialControl,
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

// runCheck runs one HTTP check and persists it under the monitor's org. The
// claim already rescheduled next_check_at, so this only records the result and
// lets the incident engine open/resolve as the streak warrants — all inside one
// WithOrgTx so RLS is armed and the check + any incident change commit together.
func (w *Worker) runCheck(ctx context.Context, m Monitor) {
	cctx, cancel := context.WithTimeout(ctx, time.Duration(m.TimeoutMs)*time.Millisecond)
	defer cancel()

	res := w.pinger.Check(cctx, m)

	// Persist with the parent ctx (not cctx): the check's own deadline has
	// served its purpose, and we don't want a borderline-timed-out check to also
	// fail to record its result.
	err := tenancy.WithOrgTx(ctx, w.pool, m.OrgID, func(tx pgx.Tx) error {
		var statusCode, latency *int
		if res.StatusCode != 0 {
			sc := res.StatusCode
			statusCode = &sc
		}
		lm := res.LatencyMs
		latency = &lm
		var errMsg *string
		if res.Error != "" {
			errMsg = &res.Error
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO check_results (org_id, monitor_id, status, status_code, latency_ms, error)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			m.OrgID, m.ID, res.Status, statusCode, latency, errMsg); err != nil {
			return err
		}
		return w.incidents.Apply(ctx, tx, m.OrgID, m.ID, res.Status)
	})
	if err != nil {
		w.log.Error("persist check", "monitor", m.ID, "org", m.OrgID, "err", err)
		// The claim already pushed next_check_at a full interval forward, so a
		// failed persist would otherwise silently drop this tick's observation
		// and skip the monitor for an entire interval. Best-effort: pull
		// next_check_at back to now so the next tick re-checks promptly. Uses a
		// detached, short-lived context so this still runs when the failure was
		// the parent ctx being cancelled (shutdown).
		w.rescheduleSoon(m)
	}
}

// rescheduleSoon best-effort resets a monitor's next_check_at to now so a check
// whose persist failed is retried on the next tick instead of waiting a full
// interval. A crash before this runs is not covered (that needs a lease/reaper),
// but transient DB errors and shutdown cancellation are.
func (w *Worker) rescheduleSoon(m Monitor) {
	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tenancy.WithOrgTx(rctx, w.pool, m.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(rctx,
			`UPDATE monitors SET next_check_at = now() WHERE org_id = $1 AND id = $2`,
			m.OrgID, m.ID)
		return err
	}); err != nil {
		w.log.Error("reschedule after failed persist", "monitor", m.ID, "org", m.OrgID, "err", err)
	}
}
