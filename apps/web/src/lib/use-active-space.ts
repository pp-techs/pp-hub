import { useParams } from "@tanstack/react-router";
import { useUiStore } from "@/lib/ui-store";

/**
 * The space the left rail is pointed at.
 *
 * Every drive surface lives under `/drive/$spaceId`, so the route param is the
 * answer almost everywhere. `/drive/account` is the exception — an account is
 * not owned by a space — and there the rail keeps showing the space the user
 * came from rather than emptying itself, which is what made Home, Recent and
 * Starred appear to lose the space before they were nested.
 *
 * Returns undefined only before the first space has ever been opened.
 */
export function useActiveSpaceId(): string | undefined {
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const lastSpaceId = useUiStore((s) => s.lastSpaceId);
  return spaceId ?? lastSpaceId ?? undefined;
}
