import { queryOptions } from "@tanstack/react-query";
import { getCommentThreads } from "@/lib/api";
import { keys } from "./keys";

/**
 * The comment threads of one markdown document, or of one subject of an
 * extension node. Read by the editor's comment store and the comments panel
 * alike, so both see the same cache; the store (`@/lib/comments`) owns
 * polling and writes.
 */
export function commentsOptions(nodeId: string, subject?: string) {
  return queryOptions({
    queryKey: keys.comments(nodeId, subject),
    queryFn: () => getCommentThreads(nodeId, subject),
    staleTime: 5_000,
  });
}
