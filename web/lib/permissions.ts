// UI mirror of the doc-05 / authz.policy permission matrix.
//
// IMPORTANT: this is convenience only. It decides which controls to *render*;
// it is NEVER the trust boundary. Every action is independently re-checked by
// the Go API via authz.Can (CLAUDE.md invariant 12). Keep this in sync with
// internal/authz/policy.go.

import type { Role } from "./types";

export type Action =
  | "org:read"
  | "org:update"
  | "org:delete"
  | "member:read"
  | "member:invite"
  | "member:remove"
  | "member:role:update"
  | "monitor:read"
  | "monitor:create"
  | "monitor:update"
  | "monitor:delete"
  | "incident:read"
  | "incident:create"
  | "incident:update"
  | "incident:resolve"
  | "statuspage:read"
  | "statuspage:publish"
  | "audit:read"
  | "billing:read"
  | "billing:manage";

const ALL: Role[] = ["owner", "admin", "member", "viewer"];
const MANAGE: Role[] = ["owner", "admin"];
const OPERATE: Role[] = ["owner", "admin", "member"];
const OWNER: Role[] = ["owner"];

const MATRIX: Record<Action, Role[]> = {
  "org:read": ALL,
  "org:update": MANAGE,
  "org:delete": ["owner"],
  "member:read": ALL,
  "member:invite": MANAGE,
  "member:remove": MANAGE,
  "member:role:update": MANAGE,
  "monitor:read": ALL,
  "monitor:create": OPERATE,
  "monitor:update": OPERATE,
  "monitor:delete": MANAGE,
  "incident:read": ALL,
  "incident:create": OPERATE,
  "incident:update": OPERATE,
  "incident:resolve": OPERATE,
  "statuspage:read": ALL,
  "statuspage:publish": MANAGE,
  "audit:read": MANAGE,
  "billing:read": MANAGE,
  "billing:manage": OWNER,
};

export function can(role: Role, action: Action): boolean {
  return MATRIX[action].includes(role);
}
