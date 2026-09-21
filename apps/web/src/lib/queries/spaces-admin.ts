// Space settings and server administration.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteSpace,
  getAdminStats,
  updateAdminUser,
  updateSpace,
  type SpaceSettings,
} from "@/lib/api";
import { keys } from "./keys";

export function useUpdateSpace(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (settings: SpaceSettings) => updateSpace(spaceId, settings),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.spaces }),
  });
}

export function useDeleteSpace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (spaceId: string) => deleteSpace(spaceId),
    // The whole space is gone: every listing keyed to it is now wrong.
    onSuccess: () => void qc.invalidateQueries(),
  });
}

export function adminStatsOptions() {
  return { queryKey: keys.adminStats, queryFn: getAdminStats };
}

export function useAdminStats() {
  return useQuery(adminStatsOptions());
}
export function useUpdateAdminUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      userId,
      ...patch
    }: {
      userId: string;
      is_admin?: boolean;
      suspended?: boolean;
    }) => updateAdminUser(userId, patch),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.users });
      void qc.invalidateQueries({ queryKey: keys.adminStats });
    },
  });
}
