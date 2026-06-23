import { NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE } from "@/lib/config";
import { loginPath } from "@/lib/nav";

// Cheap edge gate: redirect to /login when the session cookie is ABSENT on a
// protected route. It does NOT validate the session (no API call) — real
// validation is the getMe() call in the dashboard layout. This just avoids
// flashing the app shell for obviously-unauthenticated visitors.

export function middleware(req: NextRequest) {
  const path = req.nextUrl.pathname + req.nextUrl.search;

  if (!req.cookies.has(SESSION_COOKIE)) {
    return NextResponse.redirect(new URL(loginPath(path), req.url));
  }

  // Cookie present but unvalidated: forward, exposing the requested path so the
  // server layout can preserve the deep link if getMe() ultimately rejects it.
  const headers = new Headers(req.headers);
  headers.set("x-pathname", path);
  return NextResponse.next({ request: { headers } });
}

// Protect the authenticated surfaces only. Public pages, auth pages, the BFF,
// the public status page, and static assets are excluded.
export const config = {
  matcher: ["/orgs/:path*", "/account/:path*", "/invitations/:path*"],
};
