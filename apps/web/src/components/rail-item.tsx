import { cn } from "@workspace/ui/lib/utils";

/**
 * Shared geometry for every row in the left rail — quick access, the folder
 * tree and the space list. Keeping it in one place is what makes the three
 * sections read as one column rather than three stacked widgets.
 */
export function railRow(active?: boolean, className?: string) {
  return cn(
    "flex h-7 w-full items-center gap-2 rounded-md px-2 text-xs transition-colors",
    active ? "bg-accent text-foreground font-medium" : "text-foreground hover:bg-accent/60",
    className,
  );
}

/** Uppercase 11px heading that opens each rail section. */
export function RailLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-muted-foreground mb-1 px-2 text-[11px] font-medium tracking-wider uppercase">
      {children}
    </div>
  );
}
