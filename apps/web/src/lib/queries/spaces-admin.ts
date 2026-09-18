// Space settings, invitations and server administration.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createInvitation,
  deleteSpace,
  getAdminStats,
  getInvitations,
  revokeInvitation,
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

export function useInvitations(spaceId: string, enabled = true) {
  return useQuery({
    queryKey: keys.invitations(spaceId),
    queryFn: () => getInvitations(spaceId),
    enabled,
  });
}

export function useCreateInvitation(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ email, as_admin }: { email: string; as_admin: boolean }) =>
      createInvitation(spaceId, email, as_admin),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.invitations(spaceId) });
      // An existing account is added straight to the member list.
      void qc.invalidateQueries({ queryKey: keys.members(spaceId) });
    },
  });
}

export function useRevokeInvitation(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (inviteId: string) => revokeInvitation(spaceId, inviteId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.invitations(spaceId) }),
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
