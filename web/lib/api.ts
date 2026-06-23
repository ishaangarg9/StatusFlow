import "server-only";

// Public, UNAUTHENTICATED fetch for the SSR status page. This path must NEVER
// send the session cookie and is deliberately separate from lib/api/server.ts
// (which forwards the cookie). It targets the server-only INTERNAL_API_URL so
// the API origin is never exposed to the browser (ADR-11).

import type { PublicStatus } from "./types";

export type { PublicStatus };

const INTERNAL_API_URL =
  process.env.INTERNAL_API_URL ?? "http://localhost:8081";

export async function getPublicStatus(
  slug: string,
): Promise<PublicStatus | null> {
  const res = await fetch(
    `${INTERNAL_API_URL}/api/public/status/${encodeURIComponent(slug)}`,
    { cache: "no-store" },
  );
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`status ${res.status}`);
  return (await res.json()) as PublicStatus;
}
