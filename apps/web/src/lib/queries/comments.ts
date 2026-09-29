import { queryOptions } from "@tanstack/react-query";
import { getCommentThreads } from "@/lib/api";
import { keys } from "./keys";

/**
 * The comment threads of one markdown document. Read by the editor's comment
 * store and the comments panel alike, so both see the same cache; the store
 * (`@/lib/comments`) owns polling and writes.
 */
export function commentsOptions(nodeId: string) {
  return queryOptions({
    queryKey: keys.comments(nodeId),
    queryFn: () => getCommentThreads(nodeId),
    staleTime: 5_000,
  });
}
