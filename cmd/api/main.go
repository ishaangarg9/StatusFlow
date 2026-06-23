// Command api is the StatusFlow HTTP server.
package main

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/db"
	apihttp "github.com/ishaangarg9/statusflow/internal/http"
	"github.com/ishaangarg9/statusflow/internal/ops"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	log.Info("starting api", "version", version)

	cfg, err := shared.LoadConfig()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sessions := auth.NewSessionStore(pool, cfg.SessionTTL, cfg.SessionCookieName, cfg.SessionCookieSecure)
	server := apihttp.NewServer(cfg, pool, sessions, log)

	httpSrv := &stdhttp.Server{
		Addr:              cfg.APIAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Ops server: liveness/readiness/metrics on a dedicated, non-public port.
	opsSrv := ops.NewServer(cfg.OpsAddr, pool, log)

	// Run both listeners through ops.Serve so they share one graceful-shutdown
	// lifecycle. When either fails (e.g. a port bind error), cancel the other and
	// exit non-zero — a clean exit on a bind failure would hide the crash.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- ops.Serve(runCtx, httpSrv, log) }()
	go func() { errCh <- ops.Serve(runCtx, opsSrv, log) }()

	err = <-errCh // first to return: ctx cancellation (clean) or a serve failure
	cancel()      // bring the other listener down too
	<-errCh       // wait for it to finish shutting down

	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}
