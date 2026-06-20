// Package worker is the background ping loop: claim due monitors, run
// SSRF-guarded HTTP checks, write check_results + drive incidents. Two
// binaries safe under SKIP LOCKED (ADR-06).
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	pool        *pgxpool.Pool
	log         *slog.Logger
	tick        time.Duration
	batch       int
	concurrency int
	pinger      *Pinger
	incidents   *IncidentEngine
}

type Config struct {
	Tick                     time.Duration
	Batch                    int
	Concurrency              int
	IncidentOpenThreshold    int
	IncidentResolveThreshold int
}

func New(pool *pgxpool.Pool, log *slog.Logger, cfg Config) *Worker {
	return &Worker{
		pool:        pool,
		log:         log,
		tick:        cfg.Tick,
		batch:       cfg.Batch,
		concurrency: cfg.Concurrency,
		pinger:      NewPinger(),
		incidents:   NewIncidentEngine(cfg.IncidentOpenThreshold, cfg.IncidentResolveThreshold),
	}
}

// Run loops on the configured tick until ctx is cancelled.
// Each tick claims a bounded batch of due monitors and processes them in
// a bounded goroutine pool. SKIP LOCKED means N worker instances never
// double-process a row.
func (w *Worker) Run(ctx context.Context) error {
	t := time.NewTicker(w.tick)
	defer t.Stop()
	w.log.Info("worker started", "tick", w.tick, "batch", w.batch, "concurrency", w.concurrency)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			monitors, err := w.claimDue(ctx, w.batch)
			if err != nil {
				w.log.Error("claim", "err", err)
				continue
			}
			if len(monitors) == 0 {
				continue
			}
			sem := make(chan struct{}, w.concurrency)
			var wg sync.WaitGroup
			for _, m := range monitors {
				wg.Add(1)
				sem <- struct{}{}
				go func(m Monitor) {
					defer wg.Done()
					defer func() { <-sem }()
					w.runCheck(ctx, m)
				}(m)
			}
			wg.Wait()
		}
	}
}
