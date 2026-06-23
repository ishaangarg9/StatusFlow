import "server-only";

// Server transport. Used by server components, server actions, and route
// guards. Reads the `sf_session` cookie from next/headers and calls the Go API
// directly via the server-only INTERNAL_API_URL (the browser never sees it).

import { cookies } from "next/headers";
import { INTERNAL_API_URL, SESSION_COOKIE } from "../config";
import { ApiError, parseError } from "../errors";
import { makeApi, type ApiFetch } from "./endpoints";

const serverFetch: ApiFetch = async <T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> => {
  const jar = await cookies();
  const token = jar.get(SESSION_COOKIE)?.value;

  const headers: Record<string, string> = {};
  if (body !== undefined) headers["content-type"] = "application/json";
  if (token) headers["cookie"] = `${SESSION_COOKIE}=${token}`;

  const res = await fetch(`${INTERNAL_API_URL}/api${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    cache: "no-store",
  });

  if (!res.ok) throw await parseError(res);

  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
};

export { ApiError };

// Bound server client for server components, actions, and guards.
export const serverApi = makeApi(serverFetch);
