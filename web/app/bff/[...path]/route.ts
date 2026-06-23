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
import { passesOriginCheck } from "@/lib/origin";

const INTERNAL_API_URL =
  process.env.INTERNAL_API_URL ?? "http://localhost:8081";
const COOKIE_NAME = "sf_session";
// In non-production the browser↔Next hop is http://localhost, where a `Secure`
// cookie is dropped. Strip it there; keep it in production (https).
const STRIP_SECURE = process.env.NODE_ENV !== "production";

type Ctx = { params: Promise<{ path: string[] }> };

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
  const token = req.cookies.get(COOKIE_NAME)?.value;
  if (token) headers["cookie"] = `${COOKIE_NAME}=${token}`;
  const xff = req.headers.get("x-forwarded-for");
  if (xff) headers["x-forwarded-for"] = xff;

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
