// Centralized TanStack Query keys so invalidation after mutations is precise
// and typo-free. Keys are scoped by org where the data is tenant-owned.

export const qk = {
  me: ["me"] as const,
  sessions: ["sessions"] as const,

  org: (orgId: string) => ["org", orgId] as const,
  members: (orgId: string) => ["members", orgId] as const,
  invitations: (orgId: string) => ["invitations", orgId] as const,

  monitors: (orgId: string) => ["monitors", orgId] as const,
  monitor: (orgId: string, id: string) => ["monitor", orgId, id] as const,
  checks: (orgId: string, id: string) => ["checks", orgId, id] as const,

  incidents: (orgId: string) => ["incidents", orgId] as const,
  incident: (orgId: string, id: string) => ["incident", orgId, id] as const,

  statusPages: (orgId: string) => ["statusPages", orgId] as const,

  audit: (orgId: string, filters?: { action?: string; actor?: string }) =>
    ["audit", orgId, filters ?? {}] as const,
};
