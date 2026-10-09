import type * as React from "react";

/**
 * The frame every administration page shares: a title with its one-line
 * purpose, optional page-level actions, and the content below. It fills the
 * inset like the drive does — no width cap, no centering — so every page
 * starts at the same edge.
 */
export function AdminPage({
  title,
  description,
  actions,
  children,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="flex w-full flex-col gap-6 p-4 md:p-8">
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
