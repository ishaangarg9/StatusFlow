"use client";

// Provides the caller's role in the *current* org (from the [orgId] layout) to
// client components, so <Can> and nav can render role-aware controls without
// refetching /me. This is convenience UI gating only — the Go API is the trust
// boundary (CLAUDE.md invariant 12).

import { createContext, useContext } from "react";
import type { Role } from "@/lib/types";

const RoleContext = createContext<Role | null>(null);

export function RoleProvider({
  role,
  children,
}: {
  role: Role;
  children: React.ReactNode;
}) {
  return <RoleContext.Provider value={role}>{children}</RoleContext.Provider>;
}

export function useRole(): Role {
  const role = useContext(RoleContext);
  if (!role) throw new Error("useRole must be used within a RoleProvider");
  return role;
}
