// Space settings and server administration.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteAdminSetting,
  deleteSpace,
  getAdminSettings,
  getAdminStats,
  sendTestEmail,
  updateAdminSettings,
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

export function adminSettingsOptions() {
  return { queryKey: keys.adminSettings, queryFn: getAdminSettings };
}

export function useAdminSettings() {
  return useQuery(adminSettingsOptions());
}

export function useUpdateAdminSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: Record<string, unknown>) => updateAdminSettings(patch),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.adminSettings });
      void qc.invalidateQueries({ queryKey: ["admin", "security-events"] });
    },
  });
}

export function useDeleteAdminSetting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (key: string) => deleteAdminSetting(key),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.adminSettings });
      void qc.invalidateQueries({ queryKey: ["admin", "security-events"] });
    },
  });
}

export function useSendTestEmail() {
  return useMutation({ mutationFn: sendTestEmail });
}
