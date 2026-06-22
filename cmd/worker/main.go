// Command worker is the StatusFlow background pinger.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/invitations"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/worker"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

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

	// Dev transport: invitations are written to a filesystem outbox (the raw
	// token is delivered out-of-band, never logged). Swap NewOutboxMailer for a
	// real SMTP/provider transport before any non-dev deploy.
	deliverer := invitations.NewDeliverer(pool, invitations.NewOutboxMailer(""), log)

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
	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("worker", "err", err)
		os.Exit(1)
	}
}
