// Share and WOPI hooks.

import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
  createShare,
  getInheritedAccess,
  getPendingShares,
  getShareChildren,
  getShareInfo,
  getShares,
  revokePendingShare,
  revokeShare,
  type CreateShareBody,
} from "@/lib/api";
import { keys } from "./keys";

export function sharesOptions(nodeId: string | null) {
  return {
    queryKey: keys.shares(nodeId ?? ""),
    queryFn: () => getShares(nodeId!),
    enabled: nodeId !== null,
  } as const;
}

export function useShares(nodeId: string | null) {
  return useQuery(sharesOptions(nodeId));
}

/** Who reaches this node from above it. Share-gated like `useShares`. */
export function useInheritedAccess(nodeId: string | null) {
  return useQuery({
    queryKey: keys.inheritedAccess(nodeId ?? ""),
    queryFn: () => getInheritedAccess(nodeId!),
    enabled: nodeId !== null,
  });
}

/**
 * What a share changes beyond the node's own list. Listings and the node GET
 * carry `shared`, which lights the people badge; everything below the node
 * inherits the change, and which nodes those are is the server's to know, so
 * the inherited-access root goes whole. The grants list is the same table
 * seen by group, so it is refreshed alongside.
 */
function invalidateAccess(qc: QueryClient, nodeId: string) {
  void qc.invalidateQueries({ queryKey: keys.shares(nodeId) });
  void qc.invalidateQueries({ queryKey: keys.grants(nodeId) });
  void qc.invalidateQueries({ queryKey: ["inherited-access"] });
  void qc.invalidateQueries({ queryKey: keys.node(nodeId) });
  void qc.invalidateQueries({ queryKey: ["children"] });
  void qc.invalidateQueries({ queryKey: ["shared"] });
  void qc.invalidateQueries({ queryKey: keys.pendingShares(nodeId) });
  void qc.invalidateQueries({ queryKey: ["node-children"] });
}

export function useCreateShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, body }: { nodeId: string; body: CreateShareBody }) =>
      createShare(nodeId, body),
    onSuccess: (_d, { nodeId }) => invalidateAccess(qc, nodeId),
  });
}

export function useRevokeShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, shareId }: { nodeId: string; shareId: string }) =>
      revokeShare(nodeId, shareId),
    onSuccess: (_d, { nodeId }) => invalidateAccess(qc, nodeId),
  });
}

/** Invitations to addresses with no account yet. Share-gated like `useShares`. */
export function usePendingShares(nodeId: string | null) {
  return useQuery({
    queryKey: keys.pendingShares(nodeId ?? ""),
    queryFn: () => getPendingShares(nodeId!),
    enabled: nodeId !== null,
  });
}

export function useRevokePendingShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, pendingId }: { nodeId: string; pendingId: string }) =>
      revokePendingShare(nodeId, pendingId),
    onSuccess: (_d, { nodeId }) => invalidateAccess(qc, nodeId),
  });
}

// ─── Share Links ─────────────────────────────────────────────────────────────

export function shareInfoOptions(token: string) {
  return {
    queryKey: keys.shareInfo(token),
    queryFn: () => getShareInfo(token),
    retry: false,
  } as const;
}

export function useShareInfo(token: string) {
  return useQuery(shareInfoOptions(token));
}

/** Children of a folder inside a share link, for the anonymous browser. */
export function useShareChildren(token: string, dir: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: ["share-children", token, dir ?? null] as const,
    queryFn: () => getShareChildren(token, dir),
    enabled,
  });
}
