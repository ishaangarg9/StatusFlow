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
	pool         *pgxpool.Pool
	log          *slog.Logger
	tick         time.Duration
	batch        int
	concurrency  int
	leaseSeconds int
	pinger       *Pinger
	incidents    *IncidentEngine
	drainInvites func(context.Context) (int, error)
}

type Config struct {
	Tick                     time.Duration
	Batch                    int
	Concurrency              int
	ClaimLeaseSeconds        int
	IncidentOpenThreshold    int
	IncidentResolveThreshold int
	// DrainInvitations, when set, is called once per tick to deliver queued
	// invitations. Injected as a callback (rather than importing the invitations
	// domain here) so the worker stays a generic scheduler. Wired in cmd/worker
	// to invitations.Deliverer.DrainOnce.
	DrainInvitations func(context.Context) (int, error)
}

func New(pool *pgxpool.Pool, log *slog.Logger, cfg Config) *Worker {
	lease := cfg.ClaimLeaseSeconds
	if lease <= 0 {
		lease = 90
	}
	return &Worker{
		pool:         pool,
		log:          log,
		tick:         cfg.Tick,
		batch:        cfg.Batch,
		concurrency:  cfg.Concurrency,
		leaseSeconds: lease,
		pinger:       NewPinger(),
		incidents:    NewIncidentEngine(cfg.IncidentOpenThreshold, cfg.IncidentResolveThreshold),
		drainInvites: cfg.DrainInvitations,
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
			if w.drainInvites != nil {
				if n, err := w.drainInvites(ctx); err != nil {
					w.log.Error("drain invitations", "err", err)
				} else if n > 0 {
					w.log.Info("delivered invitations", "count", n)
				}
			}
			monitors, err := w.claimDue(ctx, w.batch, w.leaseSeconds)
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
