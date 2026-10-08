import { Puzzle, type LucideIcon } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { extKindOf } from "@/ext/registry";
import type { IconComponent } from "@/components/node-icon";

/** The glyph of an app: its first node kind's icon this build knows, else a puzzle piece. */
export function extIconOf(nodeKinds: readonly string[]): LucideIcon | IconComponent {
  for (const kind of nodeKinds) {
    const icon = extKindOf(kind)?.icon;
    if (icon) return icon;
  }
  return Puzzle;
}

/** A rounded tile holding an app's icon, the way the settings lists and the naming dialog show one. */
export function ExtIconTile({
  icon: Icon,
  muted,
  className,
}: {
  icon: LucideIcon | IconComponent;
  /** Greyed, for an app that is off. */
  muted?: boolean;
  className?: string;
}) {
  return (
    <span
      aria-hidden
      className={cn(
        "bg-muted flex size-9 shrink-0 items-center justify-center rounded-lg border",
        muted ? "text-muted-foreground" : "text-primary",
        className,
      )}
    >
      <Icon className="size-4" />
    </span>
  );
}
