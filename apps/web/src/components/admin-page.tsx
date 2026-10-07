import type * as React from "react";
import { cn } from "@workspace/ui/lib/utils";

/**
 * The frame every administration page shares: a title with its one-line
 * purpose, optional page-level actions, and the content below at a readable
 * width. The width is capped so a settings form does not stretch across a wide
 * monitor, while the tables get the larger cap they need.
 */
export function AdminPage({
  title,
  description,
  actions,
  narrow = false,
  children,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  /** Forms read better at a shorter measure than tables do. */
  narrow?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "mx-auto flex w-full flex-col gap-6 p-4 md:p-8",
        narrow ? "max-w-3xl" : "max-w-6xl",
      )}
    >
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="font-heading text-xl font-semibold tracking-tight">{title}</h1>
          {description && <p className="text-muted-foreground text-sm">{description}</p>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </header>
      {children}
    </div>
  );
}
