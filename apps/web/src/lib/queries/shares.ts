// Share and WOPI hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createEditSession, createShare, getShareInfo, getShares, revokeShare } from "@/lib/api";
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

export function useCreateShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, body }: { nodeId: string; body: Record<string, unknown> }) =>
      createShare(nodeId, body),
    onSuccess: (_d, { nodeId }) => void qc.invalidateQueries({ queryKey: keys.shares(nodeId) }),
  });
}

export function useRevokeShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, shareId }: { nodeId: string; shareId: string }) =>
      revokeShare(nodeId, shareId),
    onSuccess: (_d, { nodeId }) => void qc.invalidateQueries({ queryKey: keys.shares(nodeId) }),
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

// ─── WOPI ───────────────────────────────────────────────────────────────────

export function useEditSession() {
  return useMutation({
    mutationFn: (nodeId: string) => createEditSession(nodeId),
  });
}
