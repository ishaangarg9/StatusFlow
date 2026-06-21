package authz

import "github.com/google/uuid"

type AuthContext struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   Role
}

type Resource struct {
	OrgID      uuid.UUID
	TargetRole Role // target member's CURRENT role (member-management actions)
	NewRole    Role // role being assigned (member:role:update only)
}

// IsOwnershipTransfer reports whether changing a membership from currentRole to
// newRole hands ownership to the target — which requires atomically demoting
// the current owner. Lives here so role-equality logic stays inside authz
// (CLAUDE.md §2.5: no `if role == …` outside this package).
func IsOwnershipTransfer(currentRole, newRole Role) bool {
	return newRole == RoleOwner && currentRole != RoleOwner
}

// Can is the single entry point for every authorization decision.
// No handler should compare roles inline; CI greps for that pattern.
func Can(ac AuthContext, action Action, res *Resource) bool {
	// 1. matrix check (default-deny)
	if !rolePermissions[ac.Role][action] {
		return false
	}
	// 2. cross-org guard — belt to RLS's suspenders
	if res != nil && res.OrgID != uuid.Nil && res.OrgID != ac.OrgID {
		return false
	}
	// 3. structural guards (doc 05 §3.1)
	switch action {
	case ActionOrgDelete:
		return ac.Role == RoleOwner

	case ActionMemberRemove:
		// An owner is never removed directly — ownership must be transferred
		// first, otherwise the org would be left with no owner.
		if res != nil && res.TargetRole == RoleOwner {
			return false
		}

	case ActionMemberRole:
		if res == nil {
			break
		}
		// Only an owner may grant the owner role (ownership transfer).
		if res.NewRole == RoleOwner && ac.Role != RoleOwner {
			return false
		}
		// An owner's role changes ONLY via transfer (which sets NewRole=owner
		// and auto-demotes the old owner). No one — not even the owner — may
		// directly demote an owner, which would orphan the org with no owner.
		if res.TargetRole == RoleOwner && res.NewRole != RoleOwner {
			return false
		}
	}
	return true
}
