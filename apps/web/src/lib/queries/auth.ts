import { useQuery } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { getMe, logout as logoutEndpoint } from "@/lib/api";
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
 * Route-loader auth guard: fetch /me through the shared query cache
 * and redirect to /login on 401. Session is an httpOnly cookie.
 */
export async function requireAuth(qc: QueryClient): Promise<Me> {
  return qc.fetchQuery({
    queryKey: keys.me,
    queryFn: getMe,
    retry: false,
    staleTime: Infinity,
  });
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
