import { useQuery } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { ApiError, logout, setUnauthorizedHandler } from "@/lib/api";
import { getMyProfile } from "@/lib/api/users";
import { keys } from "./keys";
import type { UserProfile } from "@/lib/api";

/**
 * The one identity query. `/api/v1/users/me` is the only endpoint that answers
 * "who is this" — the thin `/api/v1/me` it replaced meant the rail and the
 * profile page held two caches of the same person that could disagree.
 *
 * Deliberately not `staleTime: Infinity`: the session lives in the database
 * now, so it can be revoked from another device. A never-stale identity is
 * exactly how a dead session went unnoticed until the next write failed.
 */
export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: getMyProfile,
    retry: false,
  });
}

/**
 * Route-loader auth guard: fetch the profile through the shared query cache
 * and, on 401, send the caller to /login carrying where they were headed.
 * Without the `redirect` param every deep link — a folder, a settings tab, a
 * shared file — resolved to the last space after signing in, which is rarely
 * what the link was for. Session is an httpOnly cookie.
 *
 * Only the first call waits on the network. After that the cached profile
 * answers at once and a stale one is revalidated in the background. The
 * guard runs in `/drive`'s `beforeLoad`, so it runs on every navigation,
 * including every `?dir=` change. Awaiting a fresh `/users/me` there held the
 * router on the previous folder for a full round trip, and that was the
 * "old folder shows first" flicker. A revoked session is still caught: the
 * background refetch 401s into the global unauthorized handler.
 */
export async function requireAuth(qc: QueryClient, href?: string): Promise<UserProfile> {
  try {
    return await qc.ensureQueryData({
      queryKey: keys.me,
      queryFn: getMyProfile,
      retry: false,
      revalidateIfStale: true,
    });
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      throw redirect({ to: "/login", search: { redirect: href ?? location.pathname } });
    }
    throw err;
  }
}

export async function fetchMe(qc: QueryClient): Promise<UserProfile | null> {
  try {
    return await qc.fetchQuery({
      queryKey: keys.me,
      queryFn: getMyProfile,
      retry: false,
    });
  } catch {
    return null;
  }
}

/**
 * Sign out of pp-hub and, when the provider supports it, of the IdP too.
 *
 * Returns `true` when it has started a navigation off this origin and the
 * caller must do nothing further; `false` when sign-out finished here and the
 * caller should route to /login.
 *
 * The ordering is load-bearing. Clearing the query cache first makes every
 * mounted query refetch, each one 401s on the session that was just revoked,
 * and the global unauthorized handler's router navigation raced the
 * cross-origin assignment below — the observable result was "Sign out"
 * landing on /login with the IdP session still standing, which is the exact
 * silent-SSO-re-entry this endpoint exists to prevent. When the document is
 * being replaced there is no cache left to clear, so the clear belongs only
 * on the branch that stays put.
 */
export async function signOut(qc: QueryClient): Promise<boolean> {
  const { end_session_url } = await logout();
  if (end_session_url) {
    // Deliberately unauthenticated from here on: a request still in flight
    // must not answer its 401 by pushing a route of its own.
    setUnauthorizedHandler(() => {});
    window.location.href = end_session_url;
    return true;
  }
  qc.clear();
  return false;
}
