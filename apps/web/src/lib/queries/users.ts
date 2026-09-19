// User management query hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import {
  updateMyProfile,
  uploadAvatar,
  searchUsers,
  listUsers,
  listSessions,
  revokeSession,
  revokeOtherSessions,
  getSpaceMembers,
  addSpaceMember,
  setMemberAdmin,
  removeSpaceMember,
} from "@/lib/api/users";
import { keys } from "./keys";
// ─── Profile ─────────────────────────────────────────────────────────────────

// Reading the profile is `useMe()` in ./auth — one identity query, one key.

export function useUpdateProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: updateMyProfile,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.me }),
  });
}

export function useUploadAvatar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: uploadAvatar,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.me }),
  });
}

// ─── Sessions ────────────────────────────────────────────────────────────────

export function useSessions() {
  return useQuery({
    queryKey: keys.sessions,
    queryFn: listSessions,
  });
}

export function useRevokeSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: revokeSession,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sessions }),
  });
}

export function useRevokeOtherSessions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: revokeOtherSessions,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sessions }),
  });
}

// ─── User Search ─────────────────────────────────────────────────────────────

export function useUserSearch(q: string, enabled = true) {
  return useQuery({
    queryKey: [...keys.userSearch, q],
    queryFn: () => searchUsers(q),
    enabled: enabled && q.length >= 2,
    staleTime: 30_000,
  });
}

// ─── Admin ───────────────────────────────────────────────────────────────────
export function usersOptions(limit = 50, offset = 0) {
  return {
    queryKey: [...keys.users, limit, offset],
    queryFn: () => listUsers(limit, offset),
  };
}

export function useUsers(limit = 50, offset = 0) {
  return useQuery(usersOptions(limit, offset));
}

// ─── Space Members ───────────────────────────────────────────────────────────

export function useSpaceMembers(spaceId: string | null) {
  return useQuery({
    queryKey: keys.members(spaceId ?? ""),
    queryFn: () => getSpaceMembers(spaceId!),
    enabled: !!spaceId,
  });
}

/**
 * Membership and group membership are the same cache tree, so all three
 * mutations below invalidate the member, invitation, and group roots rather
 * than just the member list. Admin *is* membership of the automatic Admin
 * group and joining a space *is* joining Everyone: narrowing the
 * invalidation to `members` left the People table showing a promoted admin
 * with no Admin chip until a reload.
 */
const invalidateSpace = (qc: QueryClient, spaceId: string) => {
  void qc.invalidateQueries({ queryKey: keys.members(spaceId) });
  void qc.invalidateQueries({ queryKey: keys.invitations(spaceId) });
  // The groups root also covers each group's member list.
  void qc.invalidateQueries({ queryKey: keys.groups(spaceId) });
};

export function useAddSpaceMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ spaceId, ...body }: { spaceId: string; user_id: string; as_admin: boolean }) =>
      addSpaceMember(spaceId, body),
    onSuccess: (_, vars) => invalidateSpace(qc, vars.spaceId),
  });
}

export function useSetMemberAdmin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      spaceId,
      userId,
      as_admin,
    }: {
      spaceId: string;
      userId: string;
      as_admin: boolean;
    }) => setMemberAdmin(spaceId, userId, as_admin),
    onSuccess: (_, vars) => invalidateSpace(qc, vars.spaceId),
  });
}

export function useRemoveSpaceMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ spaceId, userId }: { spaceId: string; userId: string }) =>
      removeSpaceMember(spaceId, userId),
    onSuccess: (_, vars) => invalidateSpace(qc, vars.spaceId),
  });
}
