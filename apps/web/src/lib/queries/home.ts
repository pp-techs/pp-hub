// Recent, starred, activity and storage-usage hooks. Every one is scoped to a
// single space — the route always has a space id, so none of them is optional.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getActivity, getRecent, getStarred, getUsage, starNode } from "@/lib/api";
import { keys } from "./keys";

export function useRecent(spaceId: string, limit?: number) {
  return useQuery({
    queryKey: keys.recent(spaceId, limit),
    queryFn: () => getRecent(spaceId, limit),
  });
}

export function useStarred(spaceId: string, limit?: number) {
  return useQuery({
    queryKey: keys.starred(spaceId, limit),
    queryFn: () => getStarred(spaceId, limit),
  });
}

export function useActivity(spaceId: string, limit?: number) {
  return useQuery({
    queryKey: keys.activity(spaceId, limit),
    queryFn: () => getActivity(spaceId, limit),
  });
}

export function useUsage(spaceId: string) {
  return useQuery({
    queryKey: keys.usage(spaceId),
    queryFn: () => getUsage(spaceId),
  });
}

/**
 * Toggles a star. Only the starred listing changes, so the file listings are
 * left alone — invalidating them would refetch the whole folder to reflect a
 * bookmark the row already renders optimistically.
 */
export function useToggleStar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, starred }: { nodeId: string; starred: boolean }) =>
      starNode(nodeId, starred),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["starred"] }),
  });
}
