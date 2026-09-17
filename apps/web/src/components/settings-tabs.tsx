import { Link } from "@tanstack/react-router";
import type { LinkProps } from "@tanstack/react-router";
import { cn } from "@workspace/ui/lib/utils";

export interface SettingsTab {
  label: string;
  /** Route to navigate to — typed against the generated route tree. */
  to: LinkProps["to"];
  params?: LinkProps["params"];
  /** Renders the tab in the destructive tone, for a Danger zone. */
  danger?: boolean;
}

/**
 * The tab strip every settings surface sits under.
 *
 * Tabs are links, not local state: a settings pane is a place, so it has to
 * survive a reload, be sendable to a colleague, and answer the browser's back
 * button. That is also why there is no `<Tabs>` here — the router holds the
 * active tab, and a second copy of it in component state could disagree.
 */
export function SettingsTabs({ tabs }: { tabs: SettingsTab[] }) {
  return (
    <nav
      aria-label="Settings sections"
      className="border-border flex h-9 shrink-0 items-end gap-4 border-b px-4"
    >
      {tabs.map((tab) => (
        <Link
          key={tab.label}
          to={tab.to}
          params={tab.params}
          className={cn(
            "-mb-px border-b-2 border-transparent pb-2 text-xs transition-colors",
            tab.danger
              ? "text-destructive/70 hover:text-destructive"
              : "text-muted-foreground hover:text-foreground",
          )}
          activeProps={{
            className: cn(
              "border-primary font-medium",
              tab.danger ? "text-destructive" : "text-foreground",
            ),
          }}
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  );
}
