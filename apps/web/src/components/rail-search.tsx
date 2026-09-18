import { Search } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { useUiStore } from "@/lib/ui-store";

/**
 * Trigger button for the ⌘K command palette in the navbar.
 * Opens the Command modal directly on click.
 */
export function NavSearch({ className }: { className?: string }) {
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen);

  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      onClick={() => setPaletteOpen(true)}
      className={cn(
        "text-muted-foreground hover:text-foreground h-7 w-48 justify-between gap-2 px-2 text-xs font-normal",
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-1.5">
        <Search className="size-3.5 shrink-0" />
        <span className="truncate">Search...</span>
      </div>
      <kbd className="border-border bg-muted/50 text-muted-foreground pointer-events-none shrink-0 rounded border px-1 py-0.5 text-[10px] font-mono leading-none">
        ⌘K
      </kbd>
    </Button>
  );
}

export const RailSearch = NavSearch;
