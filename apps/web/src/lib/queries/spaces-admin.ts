// Space settings and server administration.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  deleteAdminSetting,
  deleteSpace,
  getAdminSettings,
  getAdminStats,
  getLinkPreview,
  linkSpace,
  sendTestEmail,
  updateAdminSettings,
  updateAdminUser,
  updateSpace,
  type Space,
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

export function useLinkPreview(spaceId: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.linkPreview(spaceId),
    queryFn: () => getLinkPreview(spaceId),
    enabled,
  });
}

/**
 * Linking finishes in a background job: the request returns once the
 * organization exists, and the members are pushed afterwards. So the space
 * list is refetched every two seconds until this space carries its
 * `idp_org_id` — or for a minute, after which the next ordinary refetch
 * picks it up.
 */
export function useLinkSpace(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (confirmStranded: boolean) => linkSpace(spaceId, confirmStranded),
    onSuccess: () => {
      const linked = () =>
        qc.getQueryData<Space[]>(keys.spaces)?.find((s) => s.id === spaceId)?.idp_org_id;
      const started = Date.now();
      const poll = setInterval(() => {
        if (linked() || Date.now() - started > 60_000) {
          clearInterval(poll);
          void qc.invalidateQueries({ queryKey: keys.members(spaceId) });
          void qc.invalidateQueries({ queryKey: keys.departures(spaceId) });
          return;
        }
        void qc.invalidateQueries({ queryKey: keys.spaces });
      }, 2_000);
    },
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
