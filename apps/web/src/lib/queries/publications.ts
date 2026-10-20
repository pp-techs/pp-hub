// Publishing (M50): a node's publication, and the anonymous viewer's read of it.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getPublication,
  getPublicPublication,
  publishNode,
  unlockPublication,
  unpublishNode,
  updatePublication,
  type PublicationBody,
} from "@/lib/api";
import { keys } from "./keys";

/** The node's publication; `null` when it has none. */
export function usePublication(nodeId: string | null) {
  return useQuery({
    queryKey: keys.publication(nodeId ?? ""),
    queryFn: () => getPublication(nodeId!),
    enabled: nodeId !== null,
  });
}

/** Publish or republish. The answer is the publication, so it is written straight to the cache. */
export function usePublish(nodeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: PublicationBody) => publishNode(nodeId, body),
    onSuccess: (pub) => qc.setQueryData(keys.publication(nodeId), pub),
  });
}

/** Password and expiry, no new snapshot. */
export function useUpdatePublication(nodeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: PublicationBody) => updatePublication(nodeId, body),
    onSuccess: (pub) => qc.setQueryData(keys.publication(nodeId), pub),
  });
}

export function useUnpublish(nodeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unpublishNode(nodeId),
    onSuccess: () => qc.setQueryData(keys.publication(nodeId), null),
  });
}

/** What the anonymous viewer at `/p/<token>` shows. */
export function publicPublicationOptions(token: string) {
  return {
    queryKey: keys.publicPublication(token),
    queryFn: () => getPublicPublication(token),
    retry: false,
  } as const;
}

export function usePublicPublication(token: string) {
  return useQuery(publicPublicationOptions(token));
}

/** Trades the password for a frame URL; 422 means the password was wrong. */
export function useUnlockPublication(token: string) {
  return useMutation({
    mutationFn: (password: string) => unlockPublication(token, password),
  });
}
