import { useParams } from "@tanstack/react-router";
import { fmtSize } from "@/lib/api";
import { useSpaces } from "@/lib/queries";

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
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const space = spaces.data?.find((s) => s.id === spaceId);

  if (!space) return null;

  const metered = space.quota_bytes > 0;
  const pct = metered
    ? Math.min(100, Math.round((space.used_bytes / space.quota_bytes) * 100))
    : null;

  return (
    <div className="flex flex-col gap-1.5 px-2 py-1 group-data-[collapsible=icon]:hidden">
      <div className="flex items-center justify-between">
        <span className="text-muted-foreground text-[10px] font-semibold tracking-wider uppercase">
          Storage
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
      <div className="text-muted-foreground text-[10px] tabular-nums">
        {metered
          ? `${fmtSize(space.used_bytes)} of ${fmtSize(space.quota_bytes)}`
          : `${fmtSize(space.used_bytes)} used`}
      </div>
    </div>
  );
}
