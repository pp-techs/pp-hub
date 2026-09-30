import { cn } from "@workspace/ui/lib/utils";

/**
 * The page padding every drive surface shares: `p-4` at every breakpoint.
 * `Surface` uses it, and so do the folder listing's grid and
 * list, which keep their own scroller (rows scroll under a sticky header) but
 * must line up with Home, Recent, Starred, Shared and Trash.
 */
// eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind class names
export const LISTING_PADDING = "p-4";

/**
 * The scrolling body of every surface that is *not* the folder listing —
 * Home, the feeds, Trash and both settings areas — with an `@container/main`
 * so the sections inside size themselves against the column rather than the
 * viewport. That matters here because the details panel narrows this column
 * without the route knowing.
 */
export function Surface({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className="@container/main flex min-h-0 flex-1 flex-col overflow-auto">
      <div className={cn("flex flex-col gap-4 md:gap-6", LISTING_PADDING, className)}>
        {children}
      </div>
    </div>
  );
}
