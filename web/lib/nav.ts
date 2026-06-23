// Single source of truth for where an unauthenticated user is sent, so the
// middleware (edge), the server layout guard, and the client 401 handler all
// build the same /login URL and preserve the deep link via ?next=.

const LOGIN_PATH = "/login";

// safeNext keeps only same-origin, absolute paths. It rejects protocol-relative
// (`//host`) and backslash (`/\host`, `\\host`) forms, which the browser would
// otherwise resolve to an external origin — an open redirect.
export function safeNext(next: string | null | undefined): string | null {
  if (!next) return null;
  if (!next.startsWith("/")) return null;
  if (next.startsWith("//") || next.startsWith("/\\")) return null;
  return next;
}

// loginPath builds /login, carrying a validated ?next= when present.
export function loginPath(next?: string | null): string {
  const dest = safeNext(next);
  return dest ? `${LOGIN_PATH}?next=${encodeURIComponent(dest)}` : LOGIN_PATH;
}
