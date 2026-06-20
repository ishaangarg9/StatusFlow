// Typed fetch client for the Go API. Keep this thin: every endpoint is
// re-enforced server-side, so this layer is just shape + transport.
//
// For auth-sensitive calls in the browser, prefer the BFF route handlers
// under app/api/ so the sf_session cookie stays first-party + HttpOnly
// (doc 06 ADR-11). The functions below talk to the API directly and are
// safe for *server* components (SSR public page) where no cookie is sent.

const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

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

export async function getPublicStatus(
  slug: string,
): Promise<PublicStatus | null> {
  const res = await fetch(`${API_BASE_URL}/api/public/status/${slug}`, {
    // Server-side fetch only; never includes credentials.
    cache: "no-store",
  });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`status ${res.status}`);
  return (await res.json()) as PublicStatus;
}
