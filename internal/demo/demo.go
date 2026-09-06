// Package demo holds the fixed identifiers shared between the seed job
// (cmd/seed) and the demo-login shortcut (internal/domain/users), so both
// sides agree on which org/accounts "the demo" refers to without a config
// knob. None of these values are secret — knowing them grants nothing by
// itself; access is still gated by the normal membership + role a user
// actually holds.
package demo

import "github.com/google/uuid"

// OrgID is fixed (not generated) so re-running the seed job is idempotent:
// it can look the org up by primary key — which RLS allows once org_id is
// pinned via tenancy.WithOrgTx — instead of needing a privileged, cross-org
// lookup by slug.
var OrgID = uuid.MustParse("00000000-0000-4000-8000-000000000d30")

const (
	OrgName = "StatusFlow Demo"
	OrgSlug = "statusflow-demo"

	// OwnerEmail is the account the seed job uses to own and administer the
	// demo org (create monitors/incidents/status page). It is a real account
	// with a random, discarded password — it never logs in.
	OwnerEmail = "demo-owner@statusflow.internal"

	// ViewerEmail is the account POST /api/auth/demo-login signs visitors
	// into. It holds ONLY the viewer role in the demo org — everything a demo
	// visitor can't do is enforced by the existing authz matrix
	// (internal/authz/policy.go), not by this feature. No password check: the
	// account's password is random and never revealed or used for real login.
	ViewerEmail = "demo-viewer@statusflow.internal"
)
