import { useLocation, useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, ChevronRight, Info, LayoutGrid, List } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { useUiStore } from "@/lib/ui-store";
import type { NodeView, SortField, SortOrder } from "@/lib/api";

/**
 * What each path segment under `/drive/$spaceId` is called in a breadcrumb.
 */
const SECTIONS: Record<string, string> = {
  home: "Home",
  recent: "Recent",
  starred: "Starred",
  shared: "Shared with me",
  trash: "Trash",
  settings: "Settings",
  general: "General",
  people: "People",
  danger: "Danger zone",
};

const sortLabels: Record<SortField, string> = {
  name: "Name",
  modified: "Modified",
  size: "Size",
};

/**
 * The single main-column header, rendered once by the space layout:
 * breadcrumbs on the left, sort/view controls on the right (listing only).
 *
 * Self-contained on purpose — sort/order/view are URL search params and
 * detailsOpen is ui-store state, so the layout needs no props beyond the
 * space id and the listing route does not render a second copy.
 */
export function DriveHeader({ spaceId }: { spaceId: string }) {
  const navigate = useNavigate();
  const location = useLocation();
  const dirs = useUiStore((s) => s.dirs);
  const prefs = useUiStore((s) => s.listingPrefs);
  const detailsOpen = useUiStore((s) => s.detailsOpen);
  const toggleDetails = useUiStore((s) => s.toggleDetails);
  // Only the listing route declares these; absent on feeds/trash/settings.
  const {
    dir,
    sort: sortParam,
    order: orderParam,
    view: viewParam,
  } = useSearch({
    strict: false,
  }) as { dir?: string; sort?: SortField; order?: SortOrder; view?: NodeView };

  // The URL wins so a copied link keeps the view it was copied with; the
  // preference is only the fallback for a link that says nothing.
  const sort = sortParam ?? prefs.sort;
  const order = orderParam ?? prefs.order;
  const view = viewParam ?? prefs.view;

  // Sections named by the path, then — on the listing — the folder trail.
  const sections = location.pathname
    .split("/")
    .slice(3)
    .map((segment) => SECTIONS[segment])
    .filter((label): label is string => Boolean(label));

  const trail: { id: string; name: string }[] = [];
  let cursor = dir;
  const guard = new Set<string>();
  while (cursor && dirs[cursor] && !guard.has(cursor)) {
    guard.add(cursor);
    const entry = dirs[cursor];
    trail.unshift({ id: cursor, name: entry.name });
    cursor = entry.parent;
  }

  // Controls only make sense on the folder listing — every other surface is
  // a feed or a settings page with no sortable rows of its own.
  const isListing = location.pathname === `/drive/${spaceId}`;

  const setSearch = (next: Record<string, unknown>) =>
    void navigate({
      to: "/drive/$spaceId",
      params: { spaceId },
      search: (prev) => ({ ...prev, ...next }),
    });

  interface Crumb {
    key: string;
    label: string;
    /** Folder id when the crumb is clickable trail; sections are plain text. */
    dirId?: string;
  }
  const crumbs: Crumb[] = [
    ...sections.map((label) => ({ key: `section-${label}`, label })),
    ...trail.map((c) => ({ key: c.id, label: c.name, dirId: c.id })),
    // Root drive listing — named like the rail's My Drive row (Google Drive's
    // convention). Sections and folder trails already name themselves; an
    // unknown ?dir= falls through here too, which beats a blank breadcrumb.
    ...(sections.length === 0 && trail.length === 0
      ? [{ key: "my-drive", label: "My Drive" }]
      : []),
  ];

  return (
    <header className="border-border flex h-10 shrink-0 items-center justify-between gap-2 border-b px-4">
      {/* Breadcrumbs — section + folder trail only; the space switcher in the
          rail already names the space, so repeating it here reads as a
          second breadcrumb. */}
      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5 text-xs">
        {crumbs.map((crumb, i) => {
          const isLast = i === crumbs.length - 1;
          return (
            <span key={crumb.key} className="flex min-w-0 items-center gap-1.5">
              {/* The first crumb has nothing before it — the chevron is a
                  separator, not a prefix. */}
              {i > 0 && <ChevronRight className="text-border size-3.5 shrink-0" />}
              {isLast || !crumb.dirId ? (
                <span
                  className={isLast ? "truncate font-medium" : "text-muted-foreground truncate"}
                  aria-current={isLast ? "page" : undefined}
                >
                  {crumb.label}
                </span>
              ) : (
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground truncate transition-colors"
                  onClick={() =>
                    void navigate({
                      to: "/drive/$spaceId",
                      params: { spaceId },
                      search: { dir: crumb.dirId },
                    })
                  }
                >
                  {crumb.label}
                </button>
              )}
            </span>
          );
        })}
      </nav>

      {/* View controls — listing only */}
      {isListing && (
        <div className="flex shrink-0 items-center gap-1">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="sm" className="h-7 text-xs">
                  {sortLabels[sort]}
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              <DropdownMenuRadioGroup value={sort} onValueChange={(v) => setSearch({ sort: v })}>
                {(Object.keys(sortLabels) as SortField[]).map((field) => (
                  <DropdownMenuRadioItem key={field} value={field}>
                    {sortLabels[field]}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>

          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={order === "asc" ? "Sort ascending" : "Sort descending"}
            onClick={() => setSearch({ order: order === "asc" ? "desc" : "asc" })}
          >
            {order === "asc" ? <ArrowUp /> : <ArrowDown />}
          </Button>

          <div className="border-border flex items-center gap-0.5 rounded-md border p-0.5">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Grid view"
              aria-pressed={view === "grid"}
              className={cn(view === "grid" && "bg-accent text-foreground")}
              onClick={() => setSearch({ view: "grid" })}
            >
              <LayoutGrid />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="List view"
              aria-pressed={view === "list"}
              className={cn(view === "list" && "bg-accent text-foreground")}
              onClick={() => setSearch({ view: "list" })}
            >
              <List />
            </Button>
          </div>

          {/* Details is a manual toggle, not a side effect of clicking a node,
              so it sits with the other view controls. */}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Details"
            aria-pressed={detailsOpen}
            className={cn(detailsOpen && "bg-accent text-foreground")}
            onClick={toggleDetails}
          >
            <Info />
          </Button>
        </div>
      )}
    </header>
  );
}
