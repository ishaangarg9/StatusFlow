"use client";

// <Can action="monitor:create"> renders its children only if the current role
// is permitted. UI convenience only — the server independently enforces every
// action.

import { can, type Action } from "@/lib/permissions";
import { useRole } from "./role-context";

export function Can({
  action,
  children,
  fallback = null,
}: {
  action: Action;
  children: React.ReactNode;
  fallback?: React.ReactNode;
}) {
  const role = useRole();
  return <>{can(role, action) ? children : fallback}</>;
}

// Hook form for disabling/enabling controls inline.
export function useCan(action: Action): boolean {
  return can(useRole(), action);
}
