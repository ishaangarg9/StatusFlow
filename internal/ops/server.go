// Package ops exposes the operational endpoints both binaries serve on a
// dedicated, non-public port: liveness (/healthz), readiness (/readyz, which
// pings the pool), and the Prometheus scrape (/metrics). Keeping these off the
// app's public listener means k8s probes and the in-cluster scraper reach them
// while the public ingress (Cloudflare Tunnel) never exposes internal metrics.
package ops

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/metrics"
)

// shutdownGrace bounds how long Serve waits for in-flight requests to drain on
// shutdown before giving up.
const shutdownGrace = 15 * time.Second

// Serve runs srv until ctx is cancelled or the server fails, owning graceful
// shutdown. On ctx cancellation it Shutdown()s within shutdownGrace and returns
// nil; if ListenAndServe fails for any other reason — most importantly a port
// bind failure at startup — it returns that error so the caller can treat it as
// fatal and exit non-zero (a clean exit on a bind failure would hide a crash
// from the supervisor). Both binaries run every listener through this so the app
// and ops servers share one lifecycle instead of diverging per main.
func Serve(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil // a clean Shutdown, not a failure
		}
		errc <- err
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Error("shutdown", "addr", srv.Addr, "err", err)
		}
		<-errc // let ListenAndServe unwind (it returns ErrServerClosed → nil)
		return nil
	case err := <-errc:
		return err
	}
}

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
