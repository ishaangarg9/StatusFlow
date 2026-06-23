import "server-only";

// Server-side auth helpers for route guards. getMe() resolves the current user
// + memberships via the cookie-forwarding server transport; on 401 it returns
// null so layouts can redirect to /login.

import { cache } from "react";
import { serverApi, ApiError } from "./api/server";
import type { Me } from "./types";

// cache(): deduped per request, so the dashboard layout and the [orgId] layout
// share one /me round-trip.
export const getMe = cache(async (): Promise<Me | null> => {
  try {
    return await serverApi.me();
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null;
    throw err;
  }
});
