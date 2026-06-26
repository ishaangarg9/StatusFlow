// Typed endpoint layer. Each function is transport-agnostic: it takes an
// `ApiFetch` and returns parsed, typed data. Paths are API-relative (no `/api`
// or `/bff` prefix) — the transport adds its own prefix:
//   - clientFetch -> `/bff<path>`            (browser, cookie forwarded by BFF)
//   - serverFetch -> `${INTERNAL_API_URL}/api<path>` (server components/actions)
//
// makeApi(fetcher) binds a transport once and exposes the whole surface.

import type {
  AcceptResult,
  AuditPage,
  Check,
  Incident,
  IncidentUpdateStatus,
  Invitation,
  Me,
  Member,
  Monitor,
  Org,
  PublicStatus,
  Role,
  Session,
  StatusPage,
  Subscription,
} from "../types";

export type ApiFetch = <T>(
  method: string,
  path: string,
  body?: unknown,
) => Promise<T>;

export type MonitorInput = {
  name: string;
  url: string;
  method?: string;
  expectedStatus?: number;
  intervalSeconds?: number;
  timeoutMs?: number;
  isPaused?: boolean;
};

export type StatusPageInput = {
  slug?: string;
  title?: string;
  isPublic?: boolean;
  monitorIds?: string[];
};

export function makeApi(f: ApiFetch) {
  return {
    // --- auth ---
    me: () => f<Me>("GET", "/auth/me"),
    signup: (body: { email: string; password: string; name: string }) =>
      f<{ user: Me["user"] }>("POST", "/auth/signup", body),
    login: (body: { email: string; password: string }) =>
      f<{ user: Me["user"] }>("POST", "/auth/login", body),
    logout: () => f<void>("POST", "/auth/logout"),
    logoutAll: () => f<void>("POST", "/auth/logout-all"),
    listSessions: () =>
      f<{ sessions: Session[] }>("GET", "/auth/sessions").then(
        (r) => r.sessions,
      ),
    revokeSession: (id: string) => f<void>("DELETE", `/auth/sessions/${id}`),

    // --- orgs ---
    createOrg: (body: { name: string; slug: string }) =>
      f<{ org: Org }>("POST", "/orgs", body).then((r) => r.org),
    getOrg: (orgId: string) =>
      f<{ org: Org }>("GET", `/orgs/${orgId}`).then((r) => r.org),
    updateOrg: (orgId: string, body: { name?: string; slug?: string }) =>
      f<{ org: Org }>("PATCH", `/orgs/${orgId}`, body).then((r) => r.org),
    deleteOrg: (orgId: string) => f<void>("DELETE", `/orgs/${orgId}`),

    // --- members ---
    listMembers: (orgId: string) =>
      f<{ members: Member[] }>("GET", `/orgs/${orgId}/members`).then(
        (r) => r.members,
      ),
    updateMemberRole: (orgId: string, userId: string, role: Role) =>
      f<{ member: Member }>("PATCH", `/orgs/${orgId}/members/${userId}`, {
        role,
      }).then((r) => r.member),
    removeMember: (orgId: string, userId: string) =>
      f<void>("DELETE", `/orgs/${orgId}/members/${userId}`),

    // --- invitations ---
    listInvitations: (orgId: string) =>
      f<{ invitations: Invitation[] }>(
        "GET",
        `/orgs/${orgId}/invitations`,
      ).then((r) => r.invitations),
    createInvitation: (orgId: string, body: { email: string; role: Role }) =>
      f<{ invitation: Invitation }>(
        "POST",
        `/orgs/${orgId}/invitations`,
        body,
      ).then((r) => r.invitation),
    revokeInvitation: (orgId: string, id: string) =>
      f<void>("DELETE", `/orgs/${orgId}/invitations/${id}`),
    acceptInvitation: (token: string) =>
      f<{ membership: AcceptResult }>("POST", "/invitations/accept", {
        token,
      }).then((r) => r.membership),

    // --- monitors ---
    listMonitors: (orgId: string) =>
      f<{ monitors: Monitor[] }>("GET", `/orgs/${orgId}/monitors`).then(
        (r) => r.monitors,
      ),
    getMonitor: (orgId: string, id: string) =>
      f<{ monitor: Monitor }>("GET", `/orgs/${orgId}/monitors/${id}`).then(
        (r) => r.monitor,
      ),
    createMonitor: (orgId: string, body: MonitorInput) =>
      f<{ monitor: Monitor }>("POST", `/orgs/${orgId}/monitors`, body).then(
        (r) => r.monitor,
      ),
    updateMonitor: (orgId: string, id: string, body: Partial<MonitorInput>) =>
      f<{ monitor: Monitor }>(
        "PATCH",
        `/orgs/${orgId}/monitors/${id}`,
        body,
      ).then((r) => r.monitor),
    deleteMonitor: (orgId: string, id: string) =>
      f<void>("DELETE", `/orgs/${orgId}/monitors/${id}`),
    listChecks: (orgId: string, id: string, limit = 50) =>
      f<{ checks: Check[] }>(
        "GET",
        `/orgs/${orgId}/monitors/${id}/checks?limit=${limit}`,
      ).then((r) => r.checks),

    // --- incidents ---
    listIncidents: (orgId: string) =>
      f<{ incidents: Incident[] }>("GET", `/orgs/${orgId}/incidents`).then(
        (r) => r.incidents,
      ),
    getIncident: (orgId: string, id: string) =>
      f<{ incident: Incident }>("GET", `/orgs/${orgId}/incidents/${id}`).then(
        (r) => r.incident,
      ),
    createIncident: (orgId: string, body: { monitorId: string; title: string }) =>
      f<{ incident: Incident }>("POST", `/orgs/${orgId}/incidents`, body).then(
        (r) => r.incident,
      ),
    addIncidentUpdate: (
      orgId: string,
      id: string,
      body: { message: string; status: IncidentUpdateStatus },
    ) =>
      f<{ incident: Incident }>(
        "POST",
        `/orgs/${orgId}/incidents/${id}/updates`,
        body,
      ).then((r) => r.incident),
    resolveIncident: (orgId: string, id: string) =>
      f<{ incident: Incident }>(
        "POST",
        `/orgs/${orgId}/incidents/${id}/resolve`,
        {},
      ).then((r) => r.incident),

    // --- status pages ---
    listStatusPages: (orgId: string) =>
      f<{ statusPages: StatusPage[] }>(
        "GET",
        `/orgs/${orgId}/status-pages`,
      ).then((r) => r.statusPages),
    createStatusPage: (orgId: string, body: StatusPageInput) =>
      f<{ statusPage: StatusPage }>(
        "POST",
        `/orgs/${orgId}/status-pages`,
        body,
      ).then((r) => r.statusPage),
    updateStatusPage: (orgId: string, id: string, body: StatusPageInput) =>
      f<{ statusPage: StatusPage }>(
        "PATCH",
        `/orgs/${orgId}/status-pages/${id}`,
        body,
      ).then((r) => r.statusPage),
    setStatusPageMonitors: (orgId: string, id: string, monitorIds: string[]) =>
      f<{ statusPage: StatusPage }>(
        "PUT",
        `/orgs/${orgId}/status-pages/${id}/monitors`,
        { monitorIds },
      ).then((r) => r.statusPage),

    // --- audit ---
    listAudit: (
      orgId: string,
      params: { limit?: number; action?: string; actor?: string; cursor?: string } = {},
    ) => {
      const qs = new URLSearchParams();
      if (params.limit) qs.set("limit", String(params.limit));
      if (params.action) qs.set("action", params.action);
      if (params.actor) qs.set("actor", params.actor);
      if (params.cursor) qs.set("cursor", params.cursor);
      const q = qs.toString();
      return f<AuditPage>("GET", `/orgs/${orgId}/audit${q ? `?${q}` : ""}`);
    },

    // --- billing ---
    getSubscription: (orgId: string) =>
      f<{ subscription: Subscription }>(
        "GET",
        `/orgs/${orgId}/billing/subscription`,
      ).then((r) => r.subscription),
    startCheckout: (orgId: string) =>
      f<{ url: string }>("POST", `/orgs/${orgId}/billing/checkout`, {}).then(
        (r) => r.url,
      ),
    startPortal: (orgId: string) =>
      f<{ url: string }>("POST", `/orgs/${orgId}/billing/portal`, {}).then(
        (r) => r.url,
      ),

    // --- public (no cookie; used by SSR public page only) ---
    publicStatus: (slug: string) =>
      f<PublicStatus>("GET", `/public/status/${slug}`),
  };
}

export type Api = ReturnType<typeof makeApi>;
