import { useParams } from "@tanstack/react-router";
import { useMe } from "@/lib/queries";
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
 * Returns undefined only before the first space has ever been opened — and
 * for a guest, who belongs to none: a space remembered from an earlier
 * sign-in on this browser is not theirs to be shown.
 */
export function useActiveSpaceId(): string | undefined {
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const lastSpaceId = useUiStore((s) => s.lastSpaceId);
  const guest = useMe().data?.kind === "guest";
  if (guest) return undefined;
  return spaceId ?? lastSpaceId ?? undefined;
}
