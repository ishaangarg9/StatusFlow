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

// allowedOrigins: this app's own origin(s). APP_ORIGIN can pin it in prod;
// otherwise we trust the request's own computed origin (same-origin BFF).
export function passesOriginCheck(req: NextRequest): boolean {
  if (!isStateChanging(req.method)) return true;

  // Sec-Fetch-Site is set by browsers and not forgeable by page script.
  const site = req.headers.get("sec-fetch-site");
  if (site) {
    return site === "same-origin" || site === "same-site";
  }

  // Fallback: explicit Origin must match our own origin.
  const origin = req.headers.get("origin");
  if (!origin) return false; // state-changing request with no Origin -> reject

  const selfOrigin = process.env.APP_ORIGIN ?? req.nextUrl.origin;
  return origin === selfOrigin;
}
