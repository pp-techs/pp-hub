// User management query hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getMyProfile,
  updateMyProfile,
  uploadAvatar,
  searchUsers,
  listUsers,
  getSpaceMembers,
  addSpaceMember,
  setMemberAdmin,
  removeSpaceMember,
} from "@/lib/api/users";
import { keys } from "./keys";
// ─── Profile ─────────────────────────────────────────────────────────────────

export function useMyProfile() {
  return useQuery({
    queryKey: keys.profile,
    queryFn: getMyProfile,
  });
}

export function useUpdateProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: updateMyProfile,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.profile }),
  });
}

export function useUploadAvatar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: uploadAvatar,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.profile }),
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

export function useUsers(limit = 50, offset = 0) {
  return useQuery({
    queryKey: [...keys.users, limit, offset],
    queryFn: () => listUsers(limit, offset),
  });
}

// ─── Space Members ───────────────────────────────────────────────────────────

export function useSpaceMembers(spaceId: string | null) {
  return useQuery({
    queryKey: keys.members(spaceId ?? ""),
    queryFn: () => getSpaceMembers(spaceId!),
    enabled: !!spaceId,
  });
}
export function useAddSpaceMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ spaceId, ...body }: { spaceId: string; user_id: string; as_admin: boolean }) =>
      addSpaceMember(spaceId, body),
    onSuccess: (_, vars) => qc.invalidateQueries({ queryKey: keys.members(vars.spaceId) }),
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
    onSuccess: (_, vars) => qc.invalidateQueries({ queryKey: keys.members(vars.spaceId) }),
  });
}

export function useRemoveSpaceMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ spaceId, userId }: { spaceId: string; userId: string }) =>
      removeSpaceMember(spaceId, userId),
    onSuccess: (_, vars) => qc.invalidateQueries({ queryKey: keys.members(vars.spaceId) }),
  });
}
