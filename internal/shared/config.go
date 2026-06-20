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

	SessionTTL          time.Duration
	SessionCookieName   string
	SessionCookieSecure bool

	WorkerTick        time.Duration
	WorkerBatch       int
	WorkerConcurrency int

	IncidentOpenThreshold    int
	IncidentResolveThreshold int

	RateLimitLoginPerMin int
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
		SessionTTL:               time.Duration(optInt("SESSION_TTL_DAYS", 14)) * 24 * time.Hour,
		SessionCookieName:        opt("SESSION_COOKIE_NAME", "sf_session"),
		SessionCookieSecure:      optBool("SESSION_COOKIE_SECURE", false),
		WorkerTick:               time.Duration(optInt("WORKER_TICK_MS", 5000)) * time.Millisecond,
		WorkerBatch:              optInt("WORKER_BATCH", 50),
		WorkerConcurrency:        optInt("WORKER_CONCURRENCY", 20),
		IncidentOpenThreshold:    optInt("INCIDENT_OPEN_THRESHOLD", 2),
		IncidentResolveThreshold: optInt("INCIDENT_RESOLVE_THRESHOLD", 2),
		RateLimitLoginPerMin:     optInt("RATE_LIMIT_LOGIN_PER_MIN", 10),
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid config: %s", strings.Join(errs, ", "))
	}
	return c, nil
}
