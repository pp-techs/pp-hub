import { Search } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { useUiStore } from "@/lib/ui-store";

/**
 * Trigger button for the ⌘K command palette in the left rail.
 * Opens the Command modal directly on click.
 */
export function RailSearch() {
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen);

  return (
    <Button
      type="button"
      variant="outline"
      onClick={() => setPaletteOpen(true)}
      className="text-muted-foreground hover:text-foreground h-7 w-full justify-between gap-1 px-2 font-normal"
    >
      <div className="flex min-w-0 items-center gap-1.5">
        <Search data-icon="inline-start" />
        <span className="truncate">Search...</span>
      </div>
      <kbd className="border-border bg-background text-muted-foreground pointer-events-none shrink-0 rounded border px-1 py-0.5 text-[10px] font-mono leading-none">
        ⌘K
      </kbd>
    </Button>
  );
}
