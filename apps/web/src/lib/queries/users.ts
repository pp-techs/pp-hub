// User management query hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import {
  updateMyProfile,
  uploadAvatar,
  listUsers,
  listSecurityEvents,
  listSessions,
  revokeSession,
  revokeOtherSessions,
  getSpaceMembers,
  addSpaceMember,
  setMemberAdmin,
  removeSpaceMember,
  getSpaceDepartures,
  completeDeparture,
} from "@/lib/api/users";
import type { SecurityEventFilters, SecurityEventPage } from "@/lib/api";
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

/**
 * How many log rows a page holds. Fixed rather than a parameter: the cache
 * key is (filters, offset), so two callers asking for different page sizes at
 * the same offset would read each other's page.
 */
export const SECURITY_PAGE_SIZE = 50;

/**
 * A page of the instance security log. `placeholderData` keeps the previous
 * page on screen while the next one loads, so paging through a long run of
 * failed sign-ins does not blink the table away under the reader.
 */
export function useSecurityEvents(filters: SecurityEventFilters, offset = 0) {
  return useQuery({
    queryKey: keys.securityEvents(filters, offset),
    queryFn: () => listSecurityEvents({ ...filters, offset, limit: SECURITY_PAGE_SIZE }),
    placeholderData: (prev: SecurityEventPage | undefined) => prev,
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

/**
 * Membership and group membership are the same cache tree, so every mutation
 * below invalidates the member, group and departure roots rather than just
 * the member list. Admin *is* membership of the automatic Admin group and
 * joining a space *is* joining Everyone: narrowing the invalidation to
 * `members` left the Members pane showing a promoted admin with no Admin chip
 * until a reload.
 *
 * `keys.spaces` goes with them because the space DTO carries `admin_count`,
 * which is what the no-admin banner reads — demoting the last admin has to
 * raise it without a reload.
 */
const invalidateSpace = (qc: QueryClient, spaceId: string) => {
  void qc.invalidateQueries({ queryKey: keys.members(spaceId) });
  // The groups root also covers each group's member list.
  void qc.invalidateQueries({ queryKey: keys.groups(spaceId) });
  // Removing a member opens a pending handover.
  void qc.invalidateQueries({ queryKey: keys.departures(spaceId) });
  void qc.invalidateQueries({ queryKey: keys.spaces });
};

export function useAddSpaceMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ spaceId, ...body }: { spaceId: string; email: string; as_admin: boolean }) =>
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

// ─── Pending Handovers ───────────────────────────────────────────────────────

/**
 * People who have left the organization and whose content has not been handed
 * over yet. Admin-gated on the server, so the query waits on the admin check
 * rather than firing and 403-ing.
 */
export function useSpaceDepartures(spaceId: string, enabled = true) {
  return useQuery({
    queryKey: keys.departures(spaceId),
    queryFn: () => getSpaceDepartures(spaceId),
    enabled,
  });
}

export function useCompleteDeparture(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => completeDeparture(spaceId, userId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.members(spaceId) });
      void qc.invalidateQueries({ queryKey: keys.departures(spaceId) });
    },
  });
}
