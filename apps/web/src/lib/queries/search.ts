// Name search across the caller's spaces.

import { useQuery } from "@tanstack/react-query";
import { searchNodes, type SearchKind } from "@/lib/api";
import { keys } from "./keys";

/** Below this the server returns nothing, so there is no point asking. */
export const MIN_QUERY_LENGTH = 2;

interface UseSearchOptions {
  spaceId?: string;
  kind?: SearchKind;
  ownerId?: string;
  /** Days back. */
  modified?: number;
}

/**
 * Results for an already-debounced query. Debouncing belongs to the caller —
 * the palette holds both the typed value and the settled one, and only the
 * settled one reaches this hook.
 *
 * `placeholderData` keeps the previous results on screen while a new query
 * resolves, so refining a search does not blank the list on every keystroke.
 */
export function useNodeSearch(
  query: string,
  { spaceId, kind, ownerId, modified }: UseSearchOptions = {},
) {
  const trimmed = query.trim();
  return useQuery({
    queryKey: keys.search(trimmed, spaceId, kind, ownerId, modified),
    queryFn: () => searchNodes(trimmed, { spaceId, kind, ownerId, modified }),
    enabled: trimmed.length >= MIN_QUERY_LENGTH,
    placeholderData: (prev) => prev,
    staleTime: 30_000,
  });
}
