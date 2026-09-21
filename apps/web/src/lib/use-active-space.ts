import { useParams } from "@tanstack/react-router";
import { useUiStore } from "@/lib/ui-store";

/**
 * The space the left rail is pointed at.
 *
 * Every drive surface lives under `/drive/$spaceId`, so the route param is
 * the answer almost everywhere. `/drive` itself is the exception — reached
 * straight from a bookmark or after a space was deleted — and there the rail
 * keeps showing the space last opened rather than emptying itself, which is
 * what made Home, Recent and Starred appear to lose the space before they
 * were nested.
 *
 * The settings dialog reads it too: it renders from the modal container at
 * the router root, so a route param of its own is not available to it.
 *
 * Returns undefined only before the first space has ever been opened.
 */
export function useActiveSpaceId(): string | undefined {
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const lastSpaceId = useUiStore((s) => s.lastSpaceId);
  return spaceId ?? lastSpaceId ?? undefined;
}
