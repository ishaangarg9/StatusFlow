import { NextRequest, NextResponse } from "next/server";

// Cheap edge gate: redirect to /login when the session cookie is ABSENT on a
// protected route. It does NOT validate the session (no API call) — real
// validation is the getMe() call in the dashboard layout. This just avoids
// flashing the app shell for obviously-unauthenticated visitors.

const COOKIE_NAME = "sf_session";

export function middleware(req: NextRequest) {
  const hasSession = req.cookies.has(COOKIE_NAME);
  if (!hasSession) {
    const url = req.nextUrl.clone();
    const next = req.nextUrl.pathname + req.nextUrl.search;
    url.pathname = "/login";
    url.search = `?next=${encodeURIComponent(next)}`;
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}

// Protect the authenticated surfaces only. Public pages, auth pages, the BFF,
// the public status page, and static assets are excluded.
export const config = {
  matcher: ["/orgs/:path*", "/account/:path*", "/invitations/:path*"],
};
