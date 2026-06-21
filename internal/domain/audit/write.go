package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Entry is one audit row. Action is a free-form "resource:verb" string so the
// audit log can record events that are not themselves gated authz actions
// (e.g. "org:create", or system-authored "incident:open"). A nil ActorUserID
// records a system actor; a nil ResourceID is allowed.
type Entry struct {
	OrgID        uuid.UUID
	ActorUserID  uuid.UUID // uuid.Nil => system (NULL actor)
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Metadata     map[string]any
}

// Record writes an audit_logs row on the SAME transaction as the mutation it
// describes, so the trail commits atomically with the change. RLS's WITH CHECK
// requires OrgID == current_org(); callers pass the active org's id and run
// inside tenancy.WithOrgTx, so the check holds.
func Record(ctx context.Context, tx pgx.Tx, e Entry) error {
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}

	var actor any
	if e.ActorUserID != uuid.Nil {
		actor = e.ActorUserID
	}
	var resID any
	if e.ResourceID != nil {
		resID = *e.ResourceID
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (org_id, actor_user_id, action, resource_type, resource_id, metadata)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)`,
		e.OrgID, actor, e.Action, e.ResourceType, resID, raw,
	)
	if err != nil {
		return fmt.Errorf("insert audit row: %w", err)
	}
	return nil
}

// RecordGlobal writes to the GLOBAL audit sink (global_audit_logs), which has
// no org FK and no RLS, so the record survives even when the org's own
// audit_logs are cascade-deleted. Use this for events that must outlive the
// tenant, e.g. org deletion. Still written on the mutation's transaction so it
// commits atomically with the change.
func RecordGlobal(ctx context.Context, tx pgx.Tx, e Entry) error {
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal global audit metadata: %w", err)
	}
	var actor any
	if e.ActorUserID != uuid.Nil {
		actor = e.ActorUserID
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO global_audit_logs (org_id, actor_user_id, action, metadata)
		VALUES ($1, $2, $3, $4)`,
		e.OrgID, actor, e.Action, raw,
	)
	if err != nil {
		return fmt.Errorf("insert global audit row: %w", err)
	}
	return nil
}
