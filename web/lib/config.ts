// Shared transport constants. Centralized so the BFF proxy, the server
// transport, and the public SSR fetch can never drift on the cookie name or
// the API origin (a mismatch would break auth on only one path).
//
// INTERNAL_API_URL reads a NON-public env var, so Next never inlines it into
// the browser bundle; no client component imports this module anyway, so the
// API origin stays server-side (ADR-11).

export const SESSION_COOKIE = "sf_session";

export const INTERNAL_API_URL =
  process.env.INTERNAL_API_URL ?? "http://localhost:8081";
