// Package ops exposes the operational endpoints both binaries serve on a
// dedicated, non-public port: liveness (/healthz), readiness (/readyz, which
// pings the pool), and the Prometheus scrape (/metrics). Keeping these off the
// app's public listener means k8s probes and the in-cluster scraper reach them
// while the public ingress (Cloudflare Tunnel) never exposes internal metrics.
package ops

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/metrics"
)

// NewServer builds the ops HTTP server. It intentionally has its own mux (no
// authn/tenant/request-logging middleware) so probe and scrape traffic stays
// cheap and unauthenticated, and never appears in the request log.
func NewServer(addr string, pool *pgxpool.Pool, log *slog.Logger) *http.Server {
	mux := http.NewServeMux()

	// Liveness: the process is up and the runtime is responsive. Deliberately
	// does NOT touch the DB — a DB blip must not trigger a liveness restart loop.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// Readiness: can we actually serve? Gate on a bounded pool ping so a pod with
	// a dead DB connection is pulled out of rotation until it recovers.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			log.Warn("readiness ping failed", "err", err)
			http.Error(w, "not ready\n", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	mux.Handle("/metrics", metrics.Handler())

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}
