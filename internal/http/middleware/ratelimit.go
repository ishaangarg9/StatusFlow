package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/ishaangarg9/statusflow/internal/metrics"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

// visitorTTL is how long an idle limiter is kept before eviction. Long enough
// that a returning client keeps its bucket; short enough that a spray of
// distinct keys can't grow the map without bound.
const visitorTTL = 10 * time.Minute

// IPRateLimit applies a per-client-IP token bucket (requests-per-minute) and
// rejects excess traffic with 429. Suitable for /api/auth/* endpoints.
//
// The client IP is derived via ClientIP(trusted): behind a trusted proxy the
// connection peer is always the proxy, so keying on RemoteAddr alone would
// collapse into one global bucket shared by every user. Pass the trusted-proxy
// set (from config); empty means "key strictly on the connection peer".
//
// In-process; replace with Redis if you run multiple API instances and need
// shared counters. Not a substitute for upstream WAF / CDN throttling.
func IPRateLimit(perMinute int, trusted []netip.Prefix) func(http.Handler) http.Handler {
	if perMinute <= 0 {
		perMinute = 60
	}
	limit := rate.Limit(float64(perMinute) / 60.0)
	burst := perMinute

	type visitor struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}
	var (
		mu        sync.Mutex
		visitors  = map[string]*visitor{}
		nextSweep time.Time
	)

	// sweepLocked evicts limiters idle longer than visitorTTL. The key space is
	// attacker-controlled (rotating source IPs), so without eviction the map
	// grows without bound — turning an anti-abuse limiter into a memory-DoS.
	// Mirrors auth.LoginThrottle.sweepLocked; amortized to nothing by running at
	// most once per TTL. Caller holds mu.
	sweepLocked := func(now time.Time) {
		if now.Before(nextSweep) {
			return
		}
		for k, v := range visitors {
			if now.Sub(v.lastSeen) >= visitorTTL {
				delete(visitors, k)
			}
		}
		nextSweep = now.Add(visitorTTL)
	}

	get := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		sweepLocked(now)
		v, ok := visitors[ip]
		if !ok {
			v = &visitor{limiter: rate.NewLimiter(limit, burst)}
			visitors[ip] = v
		}
		v.lastSeen = now
		return v.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !get(ClientIP(r, trusted)).Allow() {
				metrics.ThrottleHits.WithLabelValues("ip").Inc()
				shared.WriteErr(w, shared.RateLimited())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the real client IP for r. When the connection peer
// (RemoteAddr) is a trusted proxy, it walks X-Forwarded-For from right to left
// and returns the first address that is NOT itself a trusted proxy — i.e. the
// closest hop the trusted chain vouches for. When the peer is not trusted (or
// no trusted proxies are configured), the peer IP is returned and XFF is
// ignored entirely, so a client can never spoof its identity by sending its own
// X-Forwarded-For.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	peer := peerIP(r)
	peerAddr, err := netip.ParseAddr(peer)
	if len(trusted) == 0 || err != nil || !trustedIP(peerAddr, trusted) {
		return peer
	}
	// Peer is trusted; consult XFF. Rightmost entries are the ones our own
	// infrastructure appended, so scan from the right and skip trusted hops.
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(parts[i])
		addr, err := netip.ParseAddr(hop)
		if err != nil {
			// Unparseable hop (e.g. an RFC 7239 "unknown" token or a trailing
			// empty element): the chain is no longer trustworthy, so fall back to
			// the peer rather than keying every such request on the garbage token.
			return peer
		}
		if !trustedIP(addr, trusted) {
			return addr.String()
		}
	}
	// Every hop was a trusted proxy; fall back to the peer.
	return peer
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trustedIP(addr netip.Addr, trusted []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
