package middleware

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/ishaangarg9/statusflow/internal/shared"
)

// IPRateLimit applies a per-IP token bucket (requests-per-minute) and rejects
// excess traffic with 429. Suitable for /api/auth/* endpoints.
//
// In-process; replace with Redis if you run multiple API instances and need
// shared counters. Not a substitute for upstream WAF / CDN throttling.
func IPRateLimit(perMinute int) func(http.Handler) http.Handler {
	if perMinute <= 0 {
		perMinute = 60
	}
	limit := rate.Limit(float64(perMinute) / 60.0)
	burst := perMinute

	var (
		mu       sync.Mutex
		visitors = map[string]*rate.Limiter{}
	)

	get := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		l, ok := visitors[ip]
		if !ok {
			l = rate.NewLimiter(limit, burst)
			visitors[ip] = l
		}
		return l
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !get(ip).Allow() {
				shared.WriteErr(w, shared.RateLimited())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
