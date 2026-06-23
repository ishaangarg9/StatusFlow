package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/ishaangarg9/statusflow/internal/metrics"
)

// Metrics records each request's latency into the Prometheus histogram, labelled
// by method, chi route *pattern* (not the raw path — keeps tenant ids/slugs out
// of the label set), and status code. Mount it inside the chi route tree so the
// pattern is resolved by the time we read it.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// chi's wrapper captures the status and transparently forwards the
		// optional ResponseWriter interfaces (Flusher/Hijacker/ReaderFrom), so
		// streaming/upgrade routes keep working under this middleware.
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // no explicit WriteHeader → implicit 200
		}
		metrics.HTTPRequestDuration.
			WithLabelValues(r.Method, route, strconv.Itoa(status)).
			Observe(time.Since(start).Seconds())
	})
}
