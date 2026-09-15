import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  addGroupMember,
  createGroup,
  deleteGroup,
  getGrants,
  getGroupMembers,
  getGroups,
  removeGroupMember,
  revokeGrant,
  updateGroup,
  upsertGrant,
} from "@/lib/api/groups";
import type { SharePerm } from "@/lib/api";
import { keys } from "./keys";

export function useGroups(spaceId: string | null) {
  return useQuery({
    queryKey: keys.groups(spaceId ?? ""),
    queryFn: () => getGroups(spaceId as string),
    enabled: Boolean(spaceId),
  });
}

export function useGroupMembers(spaceId: string, groupId: string | null) {
  return useQuery({
    queryKey: keys.groupMembers(spaceId, groupId ?? ""),
    queryFn: () => getGroupMembers(spaceId, groupId as string),
    enabled: Boolean(groupId),
  });
}

export function useCreateGroup(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string }) => createGroup(spaceId, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.groups(spaceId) }),
  });
}

export function useUpdateGroup(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { groupId: string; name: string; description?: string }) =>
      updateGroup(spaceId, v.groupId, { name: v.name, description: v.description }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.groups(spaceId) }),
  });
}

export function useDeleteGroup(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (groupId: string) => deleteGroup(spaceId, groupId),
    // Deleting a group revokes every grant it held, so what the caller can see
    // changes everywhere at once — not just in the group list.
    onSuccess: () => qc.invalidateQueries(),
  });
}

export function useAddGroupMember(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { groupId: string; userId: string }) =>
      addGroupMember(spaceId, v.groupId, v.userId),
    onSuccess: (_d, v) => {
      void qc.invalidateQueries({ queryKey: keys.groupMembers(spaceId, v.groupId) });
      void qc.invalidateQueries({ queryKey: keys.groups(spaceId) });
    },
  });
}

export function useRemoveGroupMember(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { groupId: string; userId: string }) =>
      removeGroupMember(spaceId, v.groupId, v.userId),
    onSuccess: (_d, v) => {
      void qc.invalidateQueries({ queryKey: keys.groupMembers(spaceId, v.groupId) });
      void qc.invalidateQueries({ queryKey: keys.groups(spaceId) });
    },
  });
}

// --- Grants ---

export function useGrants(nodeId: string | null) {
  return useQuery({
    queryKey: keys.grants(nodeId ?? ""),
    queryFn: () => getGrants(nodeId as string),
    enabled: Boolean(nodeId),
  });
}

/**
 * Granting or revoking changes what people can see, which the client cannot
 * predict per-query, so both invalidate broadly rather than trying to patch
 * individual caches.
 */
export function useUpsertGrant(nodeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { groupId: string; perm: SharePerm }) => upsertGrant(nodeId, v.groupId, v.perm),
    onSuccess: () => qc.invalidateQueries(),
  });
}

export function useRevokeGrant(nodeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (groupId: string) => revokeGrant(nodeId, groupId),
    onSuccess: () => qc.invalidateQueries(),
  });
}
