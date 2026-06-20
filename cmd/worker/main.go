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

	w := worker.New(pool, log, worker.Config{
		Tick:                     cfg.WorkerTick,
		Batch:                    cfg.WorkerBatch,
		Concurrency:              cfg.WorkerConcurrency,
		IncidentOpenThreshold:    cfg.IncidentOpenThreshold,
		IncidentResolveThreshold: cfg.IncidentResolveThreshold,
	})
	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("worker", "err", err)
		os.Exit(1)
	}
}
