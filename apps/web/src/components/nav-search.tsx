import { Trans } from "@lingui/react/macro";
import { Search } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";
import { useUiStore } from "@/lib/ui-store";

/**
 * Trigger button for the ⌘K command palette in the header.
 *
 * Collapses to the bare magnifier below `sm`: on a phone the full field would
 * take the whole header and squeeze the breadcrumb to nothing.
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
        "text-muted-foreground hover:text-foreground size-7 justify-between gap-2 px-0 text-xs font-normal sm:w-48 sm:px-2",
        className,
      )}
    >
      <div className="flex min-w-0 items-center gap-1.5 max-sm:justify-center max-sm:w-full">
        <Search className="size-3.5 shrink-0" />
        <span className="truncate max-sm:hidden">
          <Trans>Search...</Trans>
        </span>
      </div>
      <kbd className="border-border bg-muted/50 text-muted-foreground pointer-events-none shrink-0 rounded border px-1 py-0.5 font-mono text-[10px] leading-none max-sm:hidden">
        {/* eslint-disable-next-line lingui/no-unlocalized-strings -- key glyphs */}
        ⌘K
      </kbd>
    </Button>
  );
}
