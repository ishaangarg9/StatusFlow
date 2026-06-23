// Defense-in-depth CSRF check for the BFF. The Go API documents but does not
// implement CSRF; the first-party SameSite=Lax cookie is the baseline. For
// state-changing methods we additionally require the request to originate from
// our own origin (Sec-Fetch-Site, with an Origin allowlist fallback). This
// complements — does not replace — SameSite=Lax.

import type { NextRequest } from "next/server";

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

export function isStateChanging(method: string): boolean {
  return !SAFE_METHODS.has(method.toUpperCase());
}

export function passesOriginCheck(req: NextRequest): boolean {
  if (!isStateChanging(req.method)) return true;

  // Sec-Fetch-Site is set by browsers and not forgeable by page script. We
  // require strict `same-origin`: `same-site` would admit sibling subdomains on
  // the same registrable domain (e.g. a compromised blog.* host), which are a
  // different origin and have no business making state-changing API calls.
  const site = req.headers.get("sec-fetch-site");
  if (site) {
    return site === "same-origin";
  }

  // Fallback for clients without Sec-Fetch-Site: the explicit Origin must match
  // our own origin. In production APP_ORIGIN MUST be set to the public origin —
  // behind a TLS-terminating proxy req.nextUrl.origin is the internal host, so
  // relying on it would make the allowlist whatever the request happens to
  // report. The request-derived value is a dev-only convenience.
  const origin = req.headers.get("origin");
  if (!origin) return false; // state-changing request with no Origin -> reject

  const selfOrigin = process.env.APP_ORIGIN ?? req.nextUrl.origin;
  return origin === selfOrigin;
}
