// ky instance with same-origin cookie session.
// 401 → throw special error for auth guard.
import ky from "ky";
import type { AfterResponseState } from "ky";

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(problemDetail(message));
    this.name = "ApiError";
    this.status = status;
  }
}

/**
 * The sentence worth showing out of an error body.
 *
 * The server answers every failure as RFC 7807 problem+json, and most
 * endpoints hand the body to `ApiError` as text — which put
 * `{"type":"about:blank",…}` in front of whoever tried the action, instead
 * of the `detail` the server wrote for them ("moving it there would widen who
 * can reach it"). Parsing here, once, covers every caller; a body that is not
 * a problem document is already the message and passes through.
 */
function problemDetail(body: string): string {
  if (!body.trimStart().startsWith("{")) return body;
  try {
    const problem = JSON.parse(body) as { detail?: unknown; title?: unknown };
    if (typeof problem.detail === "string" && problem.detail) return problem.detail;
    if (typeof problem.title === "string" && problem.title) return problem.title;
  } catch {
    // Not JSON after all: the text itself is the message.
  }
  return body;
}

/**
 * What to do when the server says the session is gone. The transport cannot
 * navigate on its own — the router does not exist down here — so `main.tsx`
 * registers a handler once and every 401 goes through it. Without this a
 * session that died server-side left the SPA rendering a stale shell until
 * the next mutation happened to fail.
 */
let onUnauthorized: (() => void) | null = null;

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

export const api = ky.create({
  prefix: "/",
  credentials: "same-origin",
  throwHttpErrors: false,
  hooks: {
    afterResponse: [
      (state: AfterResponseState) => {
        if (state.response.status === 401) {
          onUnauthorized?.();
          // Still throws: route loaders (`requireAuth`) branch on ApiError.
          throw new ApiError(401, "Unauthorized");
        }
      },
    ],
  },
});
