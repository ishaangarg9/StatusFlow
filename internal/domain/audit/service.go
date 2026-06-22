package audit

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Service hosts the read API for audit_logs. The write side is colocated with
// each domain service via Record (so the audit row joins the same WithOrgTx as
// the change). audit:read is a gated action (owner/admin), enforced in handler.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type EntryView struct {
	ID           uuid.UUID  `json:"id"`
	Actor        *string    `json:"actor"` // email, or null for a system actor
	Action       string     `json:"action"`
	ResourceType *string    `json:"resourceType,omitempty"`
	ResourceID   *uuid.UUID `json:"resourceId,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// ListParams are the read-side knobs. Filters are optional and ANDed together;
// Cursor drives keyset pagination (opaque, server-issued).
type ListParams struct {
	Limit  int
	Action string     // exact action filter, e.g. "monitor:create"; empty = all
	Actor  *uuid.UUID // filter to one actor; nil = all
	Cursor string     // opaque next-page token from a prior call; empty = first page
}

// Page is one page of audit entries plus the cursor to fetch the next, older
// page. NextCursor is empty when the page is the last one.
type Page struct {
	Entries    []EntryView `json:"entries"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

// List returns a page of the org's audit entries, newest first, with optional
// action/actor filters and keyset pagination. The user join is to the global
// users table; audit_logs rows are constrained to current_org() by RLS.
//
// Pagination is keyset (not OFFSET): we order by (created_at, id) descending and
// page with a row-value comparison against the cursor, so inserts during paging
// can't shift rows across page boundaries and deep pages stay cheap.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, p ListParams) (*Page, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Build the WHERE incrementally so unset filters add no predicate.
	args := []any{orgID}
	conds := []string{"a.org_id = $1"}
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if p.Action != "" {
		add("a.action = $%d", p.Action)
	}
	if p.Actor != nil {
		add("a.actor_user_id = $%d", *p.Actor)
	}
	if p.Cursor != "" {
		ct, cid, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, shared.Validation("Invalid cursor.")
		}
		// Strictly older than the cursor row in (created_at, id) order.
		args = append(args, ct, cid)
		conds = append(conds, fmt.Sprintf("(a.created_at, a.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	// Fetch one extra row to learn whether a further page exists.
	args = append(args, limit+1)
	q := fmt.Sprintf(`
		SELECT a.id, u.email, a.action, a.resource_type, a.resource_id, a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE %s
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $%d`, strings.Join(conds, " AND "), len(args))

	out := []EntryView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v EntryView
			if err := rows.Scan(&v.ID, &v.Actor, &v.Action, &v.ResourceType, &v.ResourceID, &v.CreatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, shared.Internal(err)
	}

	page := &Page{}
	if len(out) > limit {
		last := out[limit-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
		out = out[:limit]
	}
	page.Entries = out
	return page, nil
}

// Cursor is "<RFC3339Nano>|<uuid>" base64url-encoded — opaque to clients, and
// it carries exactly the (created_at, id) tuple the keyset comparison needs.
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	ts, ids, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, fmt.Errorf("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(ids)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return t, id, nil
}
