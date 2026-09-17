import { useQuery } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { ApiError, getMe, logout as logoutEndpoint } from "@/lib/api";
import { keys } from "./keys";
import type { Me } from "@/lib/api";

export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: getMe,
    retry: false,
    staleTime: Infinity,
  });
}

/**
 * Route-loader auth guard: fetch /me through the shared query cache and, on
 * 401, send the caller to /login carrying where they were headed. Without the
 * `redirect` param every deep link — a folder, a settings tab, a shared file —
 * resolved to the last space after signing in, which is rarely what the link
 * was for. Session is an httpOnly cookie.
 */
export async function requireAuth(qc: QueryClient, href?: string): Promise<Me> {
  try {
    return await qc.fetchQuery({
      queryKey: keys.me,
      queryFn: getMe,
      retry: false,
      staleTime: Infinity,
    });
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      throw redirect({ to: "/login", search: { redirect: href ?? location.pathname } });
    }
    throw err;
  }
}

export async function fetchMe(qc: QueryClient): Promise<Me | null> {
  try {
    return await qc.fetchQuery({
      queryKey: keys.me,
      queryFn: getMe,
      retry: false,
      staleTime: Infinity,
    });
  } catch {
    return null;
  }
}

export { logoutEndpoint as logout };
