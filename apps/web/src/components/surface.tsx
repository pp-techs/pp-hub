import { cn } from "@workspace/ui/lib/utils";

/**
 * The scrolling body of every surface that is *not* the folder listing —
 * Home, the feeds, Trash and both settings areas.
 *
 * It is the single home for the page rhythm: `px-4 lg:px-6` with
 * `py-4 md:py-6`, and an `@container/main` so the sections inside size
 * themselves against the column rather than the viewport. That matters here
 * because the details panel narrows this column without the route knowing.
 *
 * The folder listing deliberately does not use it: a file manager's rows have
 * to reach the edges and scroll under a sticky header, so it keeps its own
 * full-bleed container.
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
      <div className={cn("flex flex-col gap-4 px-4 py-4 md:gap-6 md:py-6 lg:px-6", className)}>
        {children}
      </div>
    </div>
  );
}
