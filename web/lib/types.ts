// Single source of truth for the StatusFlow API's request/response shapes,
// transcribed from doc 04 (api-schema) and the Go view structs. The Go API has
// no OpenAPI spec, so these are hand-written and kept in sync by hand.

export type Role = "owner" | "admin" | "member" | "viewer";

export type ErrorEnvelope = {
  error: { code: string; message: string };
};

// --- Auth -----------------------------------------------------------------

export type User = {
  id: string;
  email: string;
  name?: string | null;
};

export type Membership = {
  orgId: string;
  orgName: string;
  orgSlug: string;
  role: Role;
  createdAt: string;
};

export type Me = {
  user: User;
  memberships: Membership[];
};

export type Session = {
  id: string;
  current: boolean;
  userAgent?: string | null;
  ip?: string | null;
  createdAt: string;
  expiresAt: string;
};

// --- Orgs -----------------------------------------------------------------

export type Org = {
  id: string;
  name: string;
  slug: string;
  createdAt: string;
  role?: Role;
};

// --- Members --------------------------------------------------------------

export type Member = {
  userId: string;
  email: string;
  name?: string | null;
  role: Role;
  createdAt: string;
};

// --- Invitations ----------------------------------------------------------

export type Invitation = {
  id: string;
  email: string;
  role: Role;
  expiresAt: string;
  createdAt: string;
};

export type AcceptResult = {
  orgId: string;
  role: Role;
};

// --- Monitors -------------------------------------------------------------

export type HttpMethod = "GET" | "HEAD" | "POST";

export type Monitor = {
  id: string;
  name: string;
  url: string;
  method: HttpMethod;
  expectedStatus: number;
  intervalSeconds: number;
  timeoutMs: number;
  isPaused: boolean;
  nextCheckAt: string;
  createdAt: string;
};

export type Check = {
  status: "up" | "down";
  statusCode?: number | null;
  latencyMs?: number | null;
  error?: string | null;
  checkedAt: string;
};

// --- Incidents ------------------------------------------------------------

export type IncidentStatus = "open" | "resolved";
export type IncidentUpdateStatus =
  | "investigating"
  | "identified"
  | "monitoring"
  | "resolved";

export type IncidentUpdate = {
  id: string;
  message: string;
  status: IncidentUpdateStatus;
  authorId?: string | null;
  createdAt: string;
};

export type Incident = {
  id: string;
  monitorId: string;
  status: IncidentStatus;
  title: string;
  startedAt: string;
  resolvedAt?: string | null;
  createdAt: string;
  updates?: IncidentUpdate[];
};

// --- Status pages ---------------------------------------------------------

export type StatusPage = {
  id: string;
  slug: string;
  title: string;
  isPublic: boolean;
  createdAt: string;
  monitorIds: string[];
};

// --- Public status (strict projection; no urls/emails/ids) ----------------

export type PublicStatus = {
  title: string;
  overall: "operational" | "degraded" | "down";
  components: { name: string; status: string }[];
  incidents: {
    title: string;
    status: string;
    startedAt: string;
    updates: { message: string; status: string; createdAt: string }[];
  }[];
};

// --- Billing --------------------------------------------------------------

export type Plan = "free" | "pro";

export type Subscription = {
  plan: Plan;
  status: string;
  currentPeriodEnd?: string | null;
  limits: { maxMonitors: number; maxStatusPages: number }; // -1 = unlimited
  usage: { monitors: number; statusPages: number };
  configured: boolean; // Stripe keys present → checkout works
  hasCustomer: boolean; // a Stripe customer exists → portal available
};

// --- Audit ----------------------------------------------------------------

export type AuditEntry = {
  id: string;
  actor?: string | null; // email, or null for a system actor
  action: string;
  resourceType?: string | null;
  resourceId?: string | null;
  createdAt: string;
};

export type AuditPage = {
  entries: AuditEntry[];
  nextCursor?: string;
};
