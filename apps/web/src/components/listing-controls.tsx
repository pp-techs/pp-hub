import { useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, Info, LayoutGrid, List } from "lucide-react";
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

const sortLabels: Record<SortField, string> = {
  name: "Name",
  modified: "Modified",
  size: "Size",
};

/**
 * Sort, view and details controls for the folder listing, portalled into the
 * shell's header. Mounted by the listing route rather than the header, so the
 * header no longer has to sniff the pathname to decide whether rows exist to
 * sort.
 *
 * Self-contained apart from the space: sort/order/view are URL search params
 * and detailsOpen is ui-store state.
 */
export function ListingControls({ spaceId }: { spaceId: string }) {
  const navigate = useNavigate();
  const prefs = useUiStore((s) => s.listingPrefs);
  const detailsOpen = useUiStore((s) => s.detailsOpen);
  const toggleDetails = useUiStore((s) => s.toggleDetails);
  const {
    sort: sortParam,
    order: orderParam,
    view: viewParam,
  } = useSearch({ strict: false }) as {
    sort?: SortField;
    order?: SortOrder;
    view?: NodeView;
  };

  // The URL wins so a copied link keeps the view it was copied with; the
  // preference is only the fallback for a link that says nothing.
  const sort = sortParam ?? prefs.sort;
  const order = orderParam ?? prefs.order;
  const view = viewParam ?? prefs.view;

  const setSearch = (next: Record<string, unknown>) =>
    void navigate({
      to: "/drive/$spaceId",
      params: { spaceId },
      search: (prev) => ({ ...prev, ...next }),
    });

  return (
    <>
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

      {/* Details is a manual toggle, not a side effect of clicking a node, so
          it sits with the other view controls. */}
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
    </>
  );
}
