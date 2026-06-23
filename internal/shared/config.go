package shared

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	APIAddr     string
	// OpsAddr is the dedicated, non-public listener for /healthz, /readyz, and
	// /metrics (internal/ops). Kept off APIAddr so the public ingress never
	// exposes internal metrics. When api+worker share a host locally, override
	// one process's OPS_ADDR to avoid a port clash.
	OpsAddr string

	SessionTTL          time.Duration
	SessionCookieName   string
	SessionCookieSecure bool

	// Resend transactional email. When ResendAPIKey is set the worker delivers
	// invitations via Resend; otherwise it falls back to the dev filesystem
	// outbox. ResendFrom is required whenever the key is set. AppBaseURL, when
	// set, is used to build a clickable accept link in the invitation email.
	ResendAPIKey string
	ResendFrom   string
	AppBaseURL   string

	WorkerTick              time.Duration
	WorkerBatch             int
	WorkerConcurrency       int
	WorkerClaimLeaseSeconds int

	IncidentOpenThreshold    int
	IncidentResolveThreshold int

	RateLimitLoginPerMin  int
	RateLimitAcceptPerMin int
}

func LoadConfig() (*Config, error) {
	var errs []string
	must := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			errs = append(errs, key)
		}
		return v
	}
	opt := func(key, def string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			return def
		}
		return v
	}
	optInt := func(key string, def int) int {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, key+" (not an int)")
			return def
		}
		return n
	}
	optBool := func(key string, def bool) bool {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, key+" (not a bool)")
			return def
		}
		return b
	}

	c := &Config{
		DatabaseURL:              must("DATABASE_URL"),
		APIAddr:                  opt("API_ADDR", ":8080"),
		OpsAddr:                  opt("OPS_ADDR", ":9090"),
		ResendAPIKey:             strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		ResendFrom:               strings.TrimSpace(os.Getenv("RESEND_FROM")),
		AppBaseURL:               strings.TrimSpace(os.Getenv("APP_BASE_URL")),
		SessionTTL:               time.Duration(optInt("SESSION_TTL_DAYS", 14)) * 24 * time.Hour,
		SessionCookieName:        opt("SESSION_COOKIE_NAME", "sf_session"),
		SessionCookieSecure:      optBool("SESSION_COOKIE_SECURE", false),
		WorkerTick:               time.Duration(optInt("WORKER_TICK_MS", 5000)) * time.Millisecond,
		WorkerBatch:              optInt("WORKER_BATCH", 50),
		WorkerConcurrency:        optInt("WORKER_CONCURRENCY", 20),
		WorkerClaimLeaseSeconds:  optInt("WORKER_CLAIM_LEASE_SECONDS", 90),
		IncidentOpenThreshold:    optInt("INCIDENT_OPEN_THRESHOLD", 2),
		IncidentResolveThreshold: optInt("INCIDENT_RESOLVE_THRESHOLD", 2),
		RateLimitLoginPerMin:     optInt("RATE_LIMIT_LOGIN_PER_MIN", 10),
		RateLimitAcceptPerMin:    optInt("RATE_LIMIT_ACCEPT_PER_MIN", 20),
	}

	// Fail fast on a half-configured transport: a Resend key with no From would
	// otherwise surface only when the worker first tries to deliver an invite.
	if c.ResendAPIKey != "" && c.ResendFrom == "" {
		errs = append(errs, "RESEND_FROM (required when RESEND_API_KEY is set)")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid config: %s", strings.Join(errs, ", "))
	}
	return c, nil
}
