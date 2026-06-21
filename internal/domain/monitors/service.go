package monitors

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Service is the monitors business layer. Every operation runs inside one
// tenancy.WithOrgTx (RLS armed on the active org) and every mutation writes an
// audit row on that same transaction (CLAUDE.md §5). Reads and writes carry an
// explicit WHERE org_id = $1 as belt-and-suspenders alongside RLS.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// MonitorView is the API projection of a monitors row.
type MonitorView struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	Method          string    `json:"method"`
	ExpectedStatus  int       `json:"expectedStatus"`
	IntervalSeconds int       `json:"intervalSeconds"`
	TimeoutMs       int       `json:"timeoutMs"`
	IsPaused        bool      `json:"isPaused"`
	NextCheckAt     time.Time `json:"nextCheckAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

// CheckView is one row from a monitor's recent check history.
type CheckView struct {
	Status     string    `json:"status"`
	StatusCode *int      `json:"statusCode,omitempty"`
	LatencyMs  *int      `json:"latencyMs,omitempty"`
	Error      *string   `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checkedAt"`
}

// CreateInput is the validated shape for issuing a monitor. Pointers mark
// optional fields so the service can apply the schema defaults.
type CreateInput struct {
	Name            string
	URL             string
	Method          string
	ExpectedStatus  *int
	IntervalSeconds *int
	TimeoutMs       *int
}

// UpdateInput patches a monitor. Every field is optional; only the non-nil
// ones are applied (PATCH semantics).
type UpdateInput struct {
	Name            *string
	URL             *string
	Method          *string
	ExpectedStatus  *int
	IntervalSeconds *int
	TimeoutMs       *int
	IsPaused        *bool
}

const minIntervalSeconds = 30

// validMethod restricts monitors to the HTTP verbs the worker is willing to
// issue. Kept conservative — these are user-supplied URLs hit by our infra.
func validMethod(m string) bool {
	switch m {
	case "GET", "HEAD", "POST":
		return true
	}
	return false
}

// validMonitorURL rejects anything that is not an absolute http(s) URL. This is
// an input-shape check; the worker's dial-time SSRF guard is the real network
// defense (it blocks loopback/private/metadata IPs after DNS resolution).
func validMonitorURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// List returns every monitor in the active org, newest first.
func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]MonitorView, error) {
	out := []MonitorView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, name, url, method, expected_status, interval_seconds,
			       timeout_ms, is_paused, next_check_at, created_at
			FROM monitors
			WHERE org_id = $1
			ORDER BY created_at DESC`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m MonitorView
			if err := rows.Scan(&m.ID, &m.Name, &m.URL, &m.Method, &m.ExpectedStatus,
				&m.IntervalSeconds, &m.TimeoutMs, &m.IsPaused, &m.NextCheckAt, &m.CreatedAt); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return out, nil
}

// Get returns a single monitor. A monitor outside the active org is invisible
// under RLS, so the lookup misses and the caller maps it to 404.
func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*MonitorView, error) {
	var m MonitorView
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, name, url, method, expected_status, interval_seconds,
			       timeout_ms, is_paused, next_check_at, created_at
			FROM monitors
			WHERE org_id = $1 AND id = $2`, orgID, id,
		).Scan(&m.ID, &m.Name, &m.URL, &m.Method, &m.ExpectedStatus,
			&m.IntervalSeconds, &m.TimeoutMs, &m.IsPaused, &m.NextCheckAt, &m.CreatedAt)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &m, nil
}

// Create inserts a monitor and audits it. Defaults mirror the schema.
func (s *Service) Create(ctx context.Context, ac authz.AuthContext, in CreateInput) (*MonitorView, error) {
	if !authz.Can(ac, authz.ActionMonitorCreate, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, shared.Validation("A monitor name is required.")
	}
	rawURL := strings.TrimSpace(in.URL)
	if !validMonitorURL(rawURL) {
		return nil, shared.Validation("A valid http(s) URL is required.")
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = "GET"
	}
	if !validMethod(method) {
		return nil, shared.Validation("Method must be one of: GET, HEAD, POST.")
	}
	expectedStatus := 200
	if in.ExpectedStatus != nil {
		expectedStatus = *in.ExpectedStatus
	}
	if expectedStatus < 100 || expectedStatus > 599 {
		return nil, shared.Validation("expectedStatus must be a valid HTTP status code.")
	}
	interval := 60
	if in.IntervalSeconds != nil {
		interval = *in.IntervalSeconds
	}
	if interval < minIntervalSeconds {
		return nil, shared.Validation("intervalSeconds must be at least 30.")
	}
	timeout := 10000
	if in.TimeoutMs != nil {
		timeout = *in.TimeoutMs
	}
	if timeout < 1000 || timeout > 60000 {
		return nil, shared.Validation("timeoutMs must be between 1000 and 60000.")
	}

	var m MonitorView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO monitors (org_id, name, url, method, expected_status, interval_seconds, timeout_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, name, url, method, expected_status, interval_seconds,
			          timeout_ms, is_paused, next_check_at, created_at`,
			ac.OrgID, name, rawURL, method, expectedStatus, interval, timeout,
		).Scan(&m.ID, &m.Name, &m.URL, &m.Method, &m.ExpectedStatus,
			&m.IntervalSeconds, &m.TimeoutMs, &m.IsPaused, &m.NextCheckAt, &m.CreatedAt); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "monitor:create",
			ResourceType: "monitor",
			ResourceID:   &m.ID,
			Metadata:     map[string]any{"name": name, "url": rawURL},
		})
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return &m, nil
}

// Update applies a partial patch to a monitor and audits it.
func (s *Service) Update(ctx context.Context, ac authz.AuthContext, id uuid.UUID, in UpdateInput) (*MonitorView, error) {
	if !authz.Can(ac, authz.ActionMonitorUpdate, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}

	// Validate the provided fields up front so a bad patch is 422, not 500.
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, shared.Validation("A monitor name cannot be empty.")
	}
	if in.URL != nil && !validMonitorURL(strings.TrimSpace(*in.URL)) {
		return nil, shared.Validation("A valid http(s) URL is required.")
	}
	if in.Method != nil && !validMethod(strings.ToUpper(strings.TrimSpace(*in.Method))) {
		return nil, shared.Validation("Method must be one of: GET, HEAD, POST.")
	}
	if in.ExpectedStatus != nil && (*in.ExpectedStatus < 100 || *in.ExpectedStatus > 599) {
		return nil, shared.Validation("expectedStatus must be a valid HTTP status code.")
	}
	if in.IntervalSeconds != nil && *in.IntervalSeconds < minIntervalSeconds {
		return nil, shared.Validation("intervalSeconds must be at least 30.")
	}
	if in.TimeoutMs != nil && (*in.TimeoutMs < 1000 || *in.TimeoutMs > 60000) {
		return nil, shared.Validation("timeoutMs must be between 1000 and 60000.")
	}

	var m MonitorView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		// COALESCE keeps the existing value when the patch field is NULL, so a
		// single statement handles any subset of fields. updated_at is bumped.
		var method *string
		if in.Method != nil {
			up := strings.ToUpper(strings.TrimSpace(*in.Method))
			method = &up
		}
		var name *string
		if in.Name != nil {
			tn := strings.TrimSpace(*in.Name)
			name = &tn
		}
		var rawURL *string
		if in.URL != nil {
			tu := strings.TrimSpace(*in.URL)
			rawURL = &tu
		}
		if err := tx.QueryRow(ctx, `
			UPDATE monitors SET
				name             = COALESCE($3, name),
				url              = COALESCE($4, url),
				method           = COALESCE($5, method),
				expected_status  = COALESCE($6, expected_status),
				interval_seconds = COALESCE($7, interval_seconds),
				timeout_ms       = COALESCE($8, timeout_ms),
				is_paused        = COALESCE($9, is_paused),
				updated_at       = now()
			WHERE org_id = $1 AND id = $2
			RETURNING id, name, url, method, expected_status, interval_seconds,
			          timeout_ms, is_paused, next_check_at, created_at`,
			ac.OrgID, id, name, rawURL, method, in.ExpectedStatus,
			in.IntervalSeconds, in.TimeoutMs, in.IsPaused,
		).Scan(&m.ID, &m.Name, &m.URL, &m.Method, &m.ExpectedStatus,
			&m.IntervalSeconds, &m.TimeoutMs, &m.IsPaused, &m.NextCheckAt, &m.CreatedAt); err != nil {
			return err // ErrNoRows -> 404
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "monitor:update",
			ResourceType: "monitor",
			ResourceID:   &id,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &m, nil
}

// Delete removes a monitor (cascading its checks/incidents) and audits it.
func (s *Service) Delete(ctx context.Context, ac authz.AuthContext, id uuid.UUID) error {
	if !authz.Can(ac, authz.ActionMonitorDelete, &authz.Resource{OrgID: ac.OrgID}) {
		return shared.Forbidden()
	}
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		cmd, err := tx.Exec(ctx, `DELETE FROM monitors WHERE org_id = $1 AND id = $2`, ac.OrgID, id)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "monitor:delete",
			ResourceType: "monitor",
			ResourceID:   &id,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound()
		}
		return shared.MapAppErr(err)
	}
	return nil
}

// Checks returns up to limit recent check results for a monitor. It first
// confirms the monitor exists in the active org (so an unknown id is 404 rather
// than an empty list that hides the difference between "no checks" and "no
// such monitor").
func (s *Service) Checks(ctx context.Context, orgID, monitorID uuid.UUID, limit int) ([]CheckView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := []CheckView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM monitors WHERE org_id = $1 AND id = $2)`,
			orgID, monitorID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		rows, err := tx.Query(ctx, `
			SELECT status, status_code, latency_ms, error, checked_at
			FROM check_results
			WHERE org_id = $1 AND monitor_id = $2
			ORDER BY checked_at DESC
			LIMIT $3`, orgID, monitorID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c CheckView
			if err := rows.Scan(&c.Status, &c.StatusCode, &c.LatencyMs, &c.Error, &c.CheckedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return out, nil
}
