package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/metrics"
)

// Metrics records each request's latency into the Prometheus histogram, labelled
// by method, chi route *pattern* (not the raw path — keeps tenant ids/slugs out
// of the label set), and status code. Mount it inside the chi route tree so the
// pattern is resolved by the time we read it.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		metrics.HTTPRequestDuration.
			WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).
			Observe(time.Since(start).Seconds())
	})
}

// statusWriter captures the response status code for the metric label. It only
// records the first WriteHeader (the one that's actually sent) and treats an
// implicit 200 (a bare Write) correctly via the default.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}
