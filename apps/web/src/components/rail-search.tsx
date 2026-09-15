import { useEffect, useRef } from "react";
import { Search } from "lucide-react";
import { useUiStore } from "@/lib/ui-store";

/**
 * Name filter for the current listing, mounted in the left rail. The ⌘K hint
 * is wired to a real shortcut so the badge reflects what the key does.
 */
export function RailSearch() {
  const query = useUiStore((s) => s.filterQuery);
  const setQuery = useUiStore((s) => s.setFilterQuery);
  const ref = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current?.focus();
        ref.current?.select();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

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
          placeholder="Search files"
          aria-label="Search files"
          className="placeholder:text-muted-foreground w-full min-w-0 border-none bg-transparent p-0 text-xs leading-none outline-none"
        />
      </div>
      <kbd className="border-border bg-background text-muted-foreground shrink-0 rounded-sm border px-1 py-0.5 text-[10px] leading-none">
        ⌘K
      </kbd>
    </div>
  );
}
