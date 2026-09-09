import { redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { ApiError, type Me } from "@/lib/api";

/**
 * Route-loader auth guard: fetch /api/v1/me through the shared query cache
 * and redirect to /login on 401. Session is an httpOnly cookie; the SPA
 * never holds tokens.
 */
export async function requireAuth(qc: QueryClient): Promise<Me> {
  try {
    return await qc.fetchQuery({
      queryKey: ["me"],
      queryFn: async () => {
        const res = await fetch("/api/v1/me");
        if (!res.ok) throw new ApiError(res.status, "unauthorized");
        return (await res.json()) as Me;
      },
      retry: false,
      staleTime: Infinity,
    });
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      throw redirect({ to: "/login" });
    }
    throw err;
  }
}

export async function fetchMe(qc: QueryClient): Promise<Me | null> {
  try {
    const me = await qc.fetchQuery({
      queryKey: ["me"],
      queryFn: async () => {
        const res = await fetch("/api/v1/me");
        if (!res.ok) throw new ApiError(res.status, "unauthorized");
        return (await res.json()) as Me;
      },
      retry: false,
      staleTime: Infinity,
    });
    return me;
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) return null;
    throw err;
  }
}
