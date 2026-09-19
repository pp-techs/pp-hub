// ky instance with same-origin cookie session.
// 401 → throw special error for auth guard.
import ky from "ky";
import type { AfterResponseState } from "ky";

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
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
