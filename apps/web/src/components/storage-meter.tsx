import { useParams } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { useSpaces } from "@/lib/queries";
import { useFormatters } from "@/lib/format";

/**
 * Storage consumption for the active space, pinned to the rail's footer.
 * `quota_bytes` is 0 for an unmetered space, in which case there is no ratio
 * to draw and only the used figure is shown.
 *
 * Hidden when the rail is collapsed to icons: a 3rem column has no room for
 * a label, a bar and a byte count.
 */
export function StorageMeter() {
  const spaces = useSpaces();
  const f = useFormatters();
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const space = spaces.data?.find((s) => s.id === spaceId);

  if (!space) return null;

  const metered = space.quota_bytes > 0;
  const pct = metered
    ? Math.min(100, Math.round((space.used_bytes / space.quota_bytes) * 100))
    : null;
  const used = f.size(space.used_bytes);
  const quota = f.size(space.quota_bytes);

  return (
    <div className="flex flex-col gap-1.5 px-2 py-1 group-data-[collapsible=icon]:hidden">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-[10px] font-semibold tracking-wider uppercase">
          <Trans>Storage</Trans>
        </span>
        {pct !== null && (
          <span className="text-muted-foreground text-[10px] tabular-nums">{pct}%</span>
        )}
      </div>
      {pct !== null && (
        <div className="bg-muted h-1 w-full overflow-hidden rounded-full">
          <div className="bg-primary h-full rounded-full" style={{ width: `${pct}%` }} />
        </div>
      )}
      {/* One message per phrasing, not "used" glued onto a formatted size:
          the two halves swap order in plenty of languages. */}
      <div className="text-muted-foreground text-[10px] tabular-nums">
        {metered ? (
          <Trans>
            {used} of {quota}
          </Trans>
        ) : (
          <Trans>{used} used</Trans>
        )}
      </div>
    </div>
  );
}
