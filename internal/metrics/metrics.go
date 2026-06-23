// Package metrics owns the Prometheus instrumentation for both binaries. It
// registers a single set of collectors against its own registry (not the global
// default) so the exposition is explicit and the package can be imported by both
// cmd/api and cmd/worker without one process's metrics polluting the other.
//
// Each process only writes the series relevant to it (the API drives the HTTP
// and throttle metrics; the worker drives checks/claims/deliveries). Series a
// given process never touches simply export as zero — normal Prometheus
// behaviour and harmless.
//
// Scrape these via the ops server (internal/ops) on a port that is NOT exposed
// through the public ingress, so internal counters never leak to the edge.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the single registry every collector in this package is registered
// against. Exposed so tests (or alternate exposition setups) can inspect it.
var Registry = prometheus.NewRegistry()

var (
	// HTTPRequestDuration is the API request histogram. The route label is the
	// chi route *pattern* (e.g. /api/orgs/{orgId}/monitors), never the raw path,
	// so tenant ids and slugs don't explode cardinality. Its _count gives request
	// totals; status="429" rows are the rate-limit/throttle rejections.
	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "statusflow",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "API request latency by method, route pattern, and status code.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// ThrottleHits counts requests rejected by an in-process limiter, by source
	// ("ip" = per-IP limiter, "login" = per-account login throttle). The HTTP
	// histogram also captures these as status="429"; this gives a direct,
	// source-attributed signal.
	ThrottleHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "statusflow",
		Subsystem: "auth",
		Name:      "throttle_hits_total",
		Help:      "Requests rejected by an in-process rate limiter, by source.",
	}, []string{"source"})

	// ChecksTotal counts monitor checks the worker has run, by outcome
	// ("up"/"down"). The headline product signal.
	ChecksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "statusflow",
		Subsystem: "worker",
		Name:      "checks_total",
		Help:      "Monitor checks executed, labelled by outcome.",
	}, []string{"result"})

	// CheckDuration is the latency of the outbound monitor check itself.
	CheckDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "statusflow",
		Subsystem: "worker",
		Name:      "check_duration_seconds",
		Help:      "Latency of an individual outbound monitor check.",
		Buckets:   prometheus.DefBuckets,
	})

	// MonitorsClaimed counts monitors claimed off the due queue (cumulative).
	MonitorsClaimed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "statusflow",
		Subsystem: "worker",
		Name:      "monitors_claimed_total",
		Help:      "Monitors claimed from the due queue (cumulative).",
	})

	// ClaimBatchSize is the size of the most recent claim batch — a cheap proxy
	// for due-queue depth/backlog per tick.
	ClaimBatchSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "statusflow",
		Subsystem: "worker",
		Name:      "claim_batch_size",
		Help:      "Monitors claimed on the most recent worker tick.",
	})

	// InvitationsProcessed counts invitation outbox rows the drain brought to a
	// terminal state — sent OR dropped (expired/already-accepted/abandoned),
	// cumulative. It is NOT a delivery-success count; failures and drops are
	// included, which is why it isn't named "delivered".
	InvitationsProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "statusflow",
		Subsystem: "worker",
		Name:      "invitations_processed_total",
		Help:      "Invitation outbox rows reaching a terminal state, sent or dropped (cumulative).",
	})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		HTTPRequestDuration,
		ThrottleHits,
		ChecksTotal,
		CheckDuration,
		MonitorsClaimed,
		ClaimBatchSize,
		InvitationsProcessed,
	)
}

// Handler serves the Prometheus exposition for this package's registry.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
