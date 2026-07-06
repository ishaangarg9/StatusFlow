// The BFF proxy. The browser talks ONLY to this Next-origin endpoint; we
// forward the first-party HttpOnly `sf_session` cookie to the Go API server-
// side, so the API origin is never exposed to the browser (ADR-11).
//
// /bff/<rest>  ->  ${INTERNAL_API_URL}/api/<rest>
//
// Load-bearing details:
//   - We forward ONLY the sf_session cookie (not the whole cookie jar).
//   - We rewrite the upstream Set-Cookie so it binds to the Next origin: in dev
//     (http://localhost) the `Secure` attribute is stripped, or the browser
//     would silently drop the login cookie. HttpOnly/SameSite/Path are kept.
//   - State-changing methods pass an Origin / Sec-Fetch-Site check first.

import { NextRequest, NextResponse } from "next/server";
import { INTERNAL_API_URL, SESSION_COOKIE } from "@/lib/config";
import { passesOriginCheck } from "@/lib/origin";

// In non-production the browser↔Next hop is http://localhost, where a `Secure`
// cookie is dropped. Strip it there; keep it in production (https).
const STRIP_SECURE = process.env.NODE_ENV !== "production";

type Ctx = { params: Promise<{ path: string[] }> };

// realClientIp resolves the caller's IP from headers our own edge/proxy set.
// The browser cannot forge CF-Connecting-IP (Cloudflare overwrites it) nor the
// values our trusted proxy appends. We deliberately do NOT return a client-
// supplied leftmost XFF value.
//
// Order matters: CF-Connecting-IP (authoritative in the Cloudflare-tunnel
// topology) first; then X-Real-Ip, which a single fronting proxy sets to the
// real downstream client; and only then the rightmost X-Forwarded-For hop —
// which in a multi-proxy chain is the *nearest proxy*, not the end user, so it's
// the weakest signal and last resort.
function realClientIp(req: NextRequest): string | undefined {
  const cf = req.headers.get("cf-connecting-ip");
  if (cf) return cf.trim();
  const real = req.headers.get("x-real-ip");
  if (real) return real.trim();
  const xff = req.headers.get("x-forwarded-for");
  if (xff) {
    const hops = xff.split(",").map((h) => h.trim()).filter(Boolean);
    if (hops.length) return hops[hops.length - 1];
  }
  return undefined;
}

async function proxy(req: NextRequest, ctx: Ctx): Promise<NextResponse> {
  if (!passesOriginCheck(req)) {
    return NextResponse.json(
      { error: { code: "csrf", message: "Cross-origin request rejected." } },
      { status: 403 },
    );
  }

  const { path } = await ctx.params;
  const target = `${INTERNAL_API_URL}/api/${(path ?? []).join("/")}${req.nextUrl.search}`;

  const headers: Record<string, string> = {};
  const ct = req.headers.get("content-type");
  if (ct) headers["content-type"] = ct;
  const accept = req.headers.get("accept");
  if (accept) headers["accept"] = accept;
  const token = req.cookies.get(SESSION_COOKIE)?.value;
  if (token) headers["cookie"] = `${SESSION_COOKIE}=${token}`;
  // Forward a SINGLE, trustworthy client IP — never the browser-supplied
  // X-Forwarded-For verbatim, which a client can spoof to poison the api's
  // per-IP rate limiter. Prefer Cloudflare's authoritative CF-Connecting-IP
  // (the edge overwrites it), else the rightmost XFF hop our own proxy chain
  // appended (Traefik/Cloudflare), else X-Real-Ip. The api trusts this only
  // because the BFF pod is in its TRUSTED_PROXIES set.
  const clientIp = realClientIp(req);
  if (clientIp) headers["x-forwarded-for"] = clientIp;

  const hasBody = req.method !== "GET" && req.method !== "HEAD";
  const body = hasBody ? await req.text() : undefined;

  const upstream = await fetch(target, {
    method: req.method,
    headers,
    body: body ? body : undefined,
    redirect: "manual",
    cache: "no-store",
  });

  const resBody = await upstream.text();
  const res = new NextResponse(resBody.length ? resBody : null, {
    status: upstream.status,
  });
  const upstreamCt = upstream.headers.get("content-type");
  if (upstreamCt) res.headers.set("content-type", upstreamCt);

  for (const sc of upstream.headers.getSetCookie()) {
    res.headers.append(
      "set-cookie",
      STRIP_SECURE ? sc.replace(/;\s*Secure/gi, "") : sc,
    );
  }

  return res;
}

export {
  proxy as GET,
  proxy as POST,
  proxy as PATCH,
  proxy as PUT,
  proxy as DELETE,
};
