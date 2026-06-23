import type { ErrorEnvelope } from "./types";

// ApiError carries the HTTP status plus the API's {error:{code,message}}
// envelope so callers can branch on status (401 -> login, 422 -> field error)
// and surface a human message in a toast.
export class ApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// parseError reads an error envelope out of a non-2xx Response. Falls back to a
// generic message when the body is empty or not the expected shape.
export async function parseError(res: Response): Promise<ApiError> {
  let code = "internal";
  let message = `Request failed (${res.status})`;
  try {
    const body = (await res.json()) as ErrorEnvelope;
    if (body?.error?.code) code = body.error.code;
    if (body?.error?.message) message = body.error.message;
  } catch {
    // empty or non-JSON body — keep the generic message.
  }
  return new ApiError(res.status, code, message);
}

// errorMessage extracts a human-readable message from an unknown thrown value,
// falling back to `fallback`. Centralizes the `e instanceof Error ? e.message`
// idiom so error presentation can evolve in one place (e.g. branching on
// ApiError.status/code) instead of being copy-pasted across every mutation.
export function errorMessage(e: unknown, fallback = "Something went wrong"): string {
  if (e instanceof Error && e.message) return e.message;
  return fallback;
}
