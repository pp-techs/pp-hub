/**
 * Validate a caller-supplied navigation target (`?redirect=`, `next=`) as a
 * path on this origin, or reject it.
 *
 * This is the SPA half of `safeNext` in
 * `apps/server/internal/auth/auth.go` — the two must stay in step, because a
 * sign-in flow hands the target across the boundary and either side accepting
 * what the other refuses is an open redirect.
 *
 * Every clause earns its place:
 *
 * - A leading "/" alone is **not** enough. `//evil.example` is a
 *   protocol-relative URL, and so is `/\evil.example`: browsers normalise a
 *   backslash to a forward slash in the authority position, so `/\` reaches a
 *   foreign origin even though it reads like a path and even though
 *   `new URL()` in some runtimes disagrees. A naive `startsWith("/")` test
 *   passes both, which is exactly the bug this replaced. Do not "simplify"
 *   the backslash case away.
 * - CR, LF and NUL never belong in a path: they split whatever consumes the
 *   value downstream (a `Location` header, a log line).
 * - Resolving against `location.origin` is the backstop for anything the
 *   prefix tests did not anticipate.
 */
export function safePath(raw: unknown): string | undefined {
  if (typeof raw !== "string" || !raw.startsWith("/")) return undefined;
  if (raw.startsWith("//") || raw.startsWith("/\\")) return undefined;
  if (/[\r\n\0]/.test(raw)) return undefined;
  try {
    if (new URL(raw, location.origin).origin !== location.origin) return undefined;
  } catch {
    return undefined;
  }
  return raw;
}
