import { useRef } from "react";
import { Search } from "lucide-react";
import { useUiStore } from "@/lib/ui-store";

/**
 * Name filter for the open folder, mounted in the left rail.
 *
 * This is deliberately *not* the ⌘K search. The field narrows what is already
 * on screen, instantly and without a request; ⌘K opens the palette, which
 * searches every space on the server. The two answer different questions, so
 * the chip is a real button through to the palette rather than a hint that
 * this field is the thing ⌘K focuses.
 */
export function RailSearch() {
  const query = useUiStore((s) => s.filterQuery);
  const setQuery = useUiStore((s) => s.setFilterQuery);
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen);
  const ref = useRef<HTMLInputElement>(null);

  return (
    <div className="border-border bg-muted/30 focus-within:border-ring focus-within:bg-background flex h-7 items-center justify-between gap-1 rounded-md border px-2 transition-colors">
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        <Search className="text-muted-foreground size-3.5 shrink-0" />
        <input
          ref={ref}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setQuery("");
              ref.current?.blur();
            }
          }}
          placeholder="Filter this folder"
          aria-label="Filter the current folder by name"
          className="placeholder:text-muted-foreground w-full min-w-0 border-none bg-transparent p-0 text-xs leading-none outline-none"
        />
      </div>
      <button
        type="button"
        title="Search every space"
        aria-label="Search every space"
        onClick={() => setPaletteOpen(true)}
        className="border-border bg-background text-muted-foreground hover:text-foreground shrink-0 rounded-sm border px-1 py-0.5 text-[10px] leading-none transition-colors"
      >
        ⌘K
      </button>
    </div>
  );
}
