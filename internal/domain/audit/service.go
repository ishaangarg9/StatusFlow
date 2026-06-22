package audit

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
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

	// A cursor is only valid for the filter set that produced it; the signature
	// is embedded in the cursor and rechecked below so a cursor replayed under
	// different filters is rejected rather than silently mixing pages.
	sig := filterSig(p)

	// Build the WHERE incrementally so unset filters add no predicate. ph() owns
	// placeholder numbering so every predicate uses the same scheme.
	args := []any{orgID}
	conds := []string{"a.org_id = $1"}
	ph := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if p.Action != "" {
		conds = append(conds, "a.action = "+ph(p.Action))
	}
	if p.Actor != nil {
		conds = append(conds, "a.actor_user_id = "+ph(*p.Actor))
	}
	if p.Cursor != "" {
		ct, cid, csig, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, shared.Validation("Invalid cursor.")
		}
		if csig != sig {
			return nil, shared.Validation("Cursor does not match the current filters.")
		}
		// Strictly older than the cursor row in (created_at, id) order.
		conds = append(conds, "(a.created_at, a.id) < ("+ph(ct)+", "+ph(cid)+")")
	}
	// Fetch one extra row to learn whether a further page exists.
	q := fmt.Sprintf(`
		SELECT a.id, u.email, a.action, a.resource_type, a.resource_id, a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE %s
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT %s`, strings.Join(conds, " AND "), ph(limit+1))

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
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID, sig)
		out = out[:limit]
	}
	page.Entries = out
	return page, nil
}

// filterSig is the canonical signature of a request's filter set, embedded in
// the cursor so a cursor can't be replayed under different filters.
func filterSig(p ListParams) string {
	actor := ""
	if p.Actor != nil {
		actor = p.Actor.String()
	}
	return p.Action + "\x1f" + actor // \x1f: unit separator, can't appear in action/uuid
}

// Cursor is "<RFC3339Nano>|<uuid>|<filterSig>" base64url-encoded — opaque to
// clients; it carries the (created_at, id) tuple the keyset needs plus the
// filter signature it was issued under.
func encodeCursor(t time.Time, id uuid.UUID, sig string) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id.String() + "|" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, uuid.UUID, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, "", err
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		return time.Time{}, uuid.Nil, "", fmt.Errorf("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, "", err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, "", err
	}
	return t, id, parts[2], nil
}
