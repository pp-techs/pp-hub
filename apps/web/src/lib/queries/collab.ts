import { getCollabSession } from "@/lib/api";
import { keys } from "./keys";

/**
 * The block editor's room for a markdown document. Never served from cache:
 * the answer is re-read on every disconnect to learn whether access survived
 * and whether the room was reloaded under a new epoch, so a remembered one
 * would hide exactly what the re-read is for. `retry: false` because a 4xx
 * here is the answer ("no access"), not a blip.
 */
export function collabOptions(nodeId: string) {
  return {
    queryKey: keys.collab(nodeId),
    queryFn: () => getCollabSession(nodeId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  } as const;
}
