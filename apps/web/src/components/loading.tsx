import type { ReactNode } from "react";
import { Trans } from "@lingui/react/macro";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { TableCell, TableRow } from "@workspace/ui/components/table";
import { cn } from "@workspace/ui/lib/utils";

/*
 * Every surface's loading state comes from here, or from a skeleton that sits
 * next to the component it stands in for (`NodeGridSkeleton`,
 * `NodeListSkeleton`) because it shares that component's layout constants.
 * A bare "Loading…" line is not a loading state: the real content then lands
 * at a different height and shoves everything below it.
 */

/** The one announcement a screen reader gets for a region that is loading. */
export function LoadingStatus({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div role="status" aria-busy="true" className={className}>
      <span className="sr-only">
        <Trans>Loading…</Trans>
      </span>
      {children}
    </div>
  );
}

/** Deterministic, uneven widths: identical bars read as a progress meter. */
function lineWidth(i: number, min = 45, spread = 45): string {
  return `${min + ((i * 37) % spread)}%`;
}

/**
 * Body rows for a shadcn `Table` whose real header is already on screen. The
 * caller keeps its own `TableHeader`, so column widths are the real ones.
 */
export function SkeletonTableRows({ columns, rows = 6 }: { columns: number; rows?: number }) {
  return (
    <>
      {Array.from({ length: rows }, (_, r) => (
        <TableRow key={r} className="hover:bg-transparent">
          {Array.from({ length: columns }, (_, c) => (
            <TableCell key={c}>
              {r === 0 && c === 0 && (
                <span role="status" className="sr-only">
                  <Trans>Loading…</Trans>
                </span>
              )}
              <Skeleton
                className="h-3.5"
                style={{ width: c === 0 ? lineWidth(r, 40, 40) : lineWidth(r + c, 50, 40) }}
              />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  );
}

/**
 * The thin bar a surface shows while it refetches data it is already
 * displaying. It sits `sticky` at the top of the scroller and takes no height,
 * so turning it on and off never moves a row.
 */
export function FetchingBar({ active }: { active: boolean }) {
  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none sticky top-0 z-30 -mb-0.5 h-0.5 w-full overflow-hidden transition-opacity duration-300",
        active ? "opacity-100" : "opacity-0",
      )}
    >
      {active && (
        <div className="bg-primary animate-indeterminate h-full w-1/3 motion-reduce:w-full motion-reduce:animate-pulse" />
      )}
    </div>
  );
}
