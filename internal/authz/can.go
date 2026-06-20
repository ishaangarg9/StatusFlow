package authz

import "github.com/google/uuid"

type AuthContext struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   Role
}

type Resource struct {
	OrgID      uuid.UUID
	TargetRole Role // set for member-management actions
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
	case ActionMemberRemove, ActionMemberRole:
		if res != nil && res.TargetRole == RoleOwner && ac.Role != RoleOwner {
			return false // only the owner may touch the owner (ownership transfer)
		}
	}
	return true
}
