// Command api is the StatusFlow HTTP server.
package main

import (
	"context"
	"errors"
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

	errCh := make(chan error, 2)
	go func() {
		log.Info("api listening", "addr", cfg.APIAddr)
		errCh <- httpSrv.ListenAndServe()
	}()
	go func() {
		log.Info("ops listening", "addr", cfg.OpsAddr)
		errCh <- opsSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal")
	case err := <-errCh:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			log.Error("listen", "err", err)
		}
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	if err := opsSrv.Shutdown(shutCtx); err != nil {
		log.Error("ops shutdown", "err", err)
	}
}
