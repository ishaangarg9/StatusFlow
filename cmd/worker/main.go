// Command worker is the StatusFlow background pinger.
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

	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/invitations"
	"github.com/ishaangarg9/statusflow/internal/ops"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/worker"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	log.Info("starting worker", "version", version)

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

	// Invitation transport: Resend when configured, else the dev filesystem
	// outbox. Either way the raw token leaves the process ONLY through the Mailer
	// and is never logged or persisted.
	mailer := pickMailer(cfg, log)
	deliverer := invitations.NewDeliverer(pool, mailer, log)

	// Ops server: liveness/readiness/metrics on a dedicated, non-public port.
	opsSrv := ops.NewServer(cfg.OpsAddr, pool, log)
	go func() {
		log.Info("ops listening", "addr", cfg.OpsAddr)
		if err := opsSrv.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			log.Error("ops listen", "err", err)
		}
	}()

	w := worker.New(pool, log, worker.Config{
		Tick:                     cfg.WorkerTick,
		Batch:                    cfg.WorkerBatch,
		Concurrency:              cfg.WorkerConcurrency,
		ClaimLeaseSeconds:        cfg.WorkerClaimLeaseSeconds,
		IncidentOpenThreshold:    cfg.IncidentOpenThreshold,
		IncidentResolveThreshold: cfg.IncidentResolveThreshold,
		DrainInvitations: func(ctx context.Context) (int, error) {
			return deliverer.DrainOnce(ctx, cfg.WorkerBatch, cfg.WorkerClaimLeaseSeconds)
		},
	})

	// Run blocks until ctx is cancelled (SIGTERM); the worker's own loop drains
	// in-flight checks before returning (see worker.Run).
	runErr := w.Run(ctx)

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := opsSrv.Shutdown(shutCtx); err != nil {
		log.Error("ops shutdown", "err", err)
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Error("worker", "err", runErr)
		os.Exit(1)
	}
}

// pickMailer returns the Resend transport when an API key is configured,
// otherwise the dev filesystem outbox. Config validation already guarantees
// RESEND_FROM is present whenever the key is set.
func pickMailer(cfg *shared.Config, log *slog.Logger) invitations.Mailer {
	if cfg.ResendAPIKey != "" {
		log.Info("invitation transport: resend", "from", cfg.ResendFrom)
		return invitations.NewResendMailer(cfg.ResendAPIKey, cfg.ResendFrom, cfg.AppBaseURL)
	}
	log.Warn("invitation transport: dev filesystem outbox (set RESEND_API_KEY for real delivery)")
	return invitations.NewOutboxMailer("")
}
