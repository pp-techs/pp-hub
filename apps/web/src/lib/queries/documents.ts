import { useQuery } from "@tanstack/react-query";
import { getNodeDocument, getShareDocument } from "@/lib/api";
import { keys } from "./keys";

/**
 * A markdown file parsed on the server, for the read-only renderer. The
 * current version refetches on every mount (the collab room writes new
 * versions behind it); a past version is immutable, so it is never stale.
 * `retry: false` because a 4xx here is the answer — not markdown, no
 * download capability, a version of some other node — not a blip.
 */
export function nodeDocumentOptions(nodeId: string | null, versionId?: string) {
  return {
    queryKey: keys.document(nodeId ?? "", versionId),
    queryFn: () => getNodeDocument(nodeId!, versionId),
    enabled: nodeId !== null,
    staleTime: versionId ? Infinity : 0,
    retry: false,
  } as const;
}

export function useNodeDocument(nodeId: string | null, versionId?: string) {
  return useQuery(nodeDocumentOptions(nodeId, versionId));
}

/** A markdown file inside a share link, as an anonymous visitor reads it. */
export function useShareDocument(token: string, nodeId: string | null) {
  return useQuery({
    queryKey: keys.shareDocument(token, nodeId ?? ""),
    queryFn: () => getShareDocument(token, nodeId!),
    enabled: nodeId !== null,
    retry: false,
  });
}
