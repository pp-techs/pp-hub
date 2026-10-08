import { cn } from "@workspace/ui/lib/utils";
import { LABEL_COLORS } from "./board-state";
import type { Label } from "./api";

/** The small uppercase caption over a field or a section of a dialog. */
export const CAPTION =
  // eslint-disable-next-line lingui/no-unlocalized-strings -- class names, not copy
  "text-muted-foreground text-[0.625rem] font-semibold tracking-wider uppercase";

/** A label as a small chip: a tinted pill with a dot in the label's colour. */
export function LabelChip({ label, className }: { label: Label; className?: string }) {
  return (
    <span
      className={cn(
        "text-foreground inline-flex max-w-full items-center gap-1 rounded-full border px-1.5 py-px text-[0.625rem] leading-4",
        className,
      )}
      style={{ borderColor: `${label.color}66`, backgroundColor: `${label.color}1f` }}
    >
      <span
        aria-hidden
        className="size-1.5 shrink-0 rounded-full"
        style={{ backgroundColor: label.color }}
      />
      <span className="truncate">{label.name}</span>
    </span>
  );
}

/** The fixed palette a label colour is picked from. */
export function ColorPicker({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (color: string) => void;
  disabled?: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      {LABEL_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          disabled={disabled}
          aria-label={color}
          aria-pressed={value.toLowerCase() === color}
          onClick={() => onChange(color)}
          className={cn(
            "size-4 rounded-full ring-offset-2 transition-shadow focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50",
            "ring-offset-background",
            value.toLowerCase() === color && "ring-foreground ring-2",
          )}
          style={{ backgroundColor: color }}
        />
      ))}
    </div>
  );
}
