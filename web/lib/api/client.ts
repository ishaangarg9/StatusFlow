// Browser transport. Talks ONLY to the Next-origin BFF (`/bff/*`), which
// forwards the first-party HttpOnly `sf_session` cookie to the Go API. The
// browser never sees the API origin (ADR-11). On 401 (session expired/revoked)
// we hard-redirect to /login.

import { ApiError, parseError } from "../errors";
import { makeApi, type ApiFetch } from "./endpoints";

const clientFetch: ApiFetch = async <T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> => {
  const res = await fetch(`/bff${path}`, {
    method,
    credentials: "include",
    headers: body !== undefined ? { "content-type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401) {
    if (typeof window !== "undefined") {
      window.location.href = "/login";
    }
    throw new ApiError(401, "unauthorized", "Your session has expired.");
  }
  if (!res.ok) throw await parseError(res);

  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
};

// Bound client for use inside client components / TanStack Query functions.
export const api = makeApi(clientFetch);
