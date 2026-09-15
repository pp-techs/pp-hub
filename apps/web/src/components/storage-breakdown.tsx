import { fmtSize, type UsageCategory } from "@/lib/api";
import { cn } from "@workspace/ui/lib/utils";

/**
 * Storage split by MIME family.
 *
 * The design calls for one cyan segment and the rest in neutral greys: the
 * accent marks the largest category so the eye lands on what is actually
 * filling the disk, and the others stay quiet. There is no categorical colour
 * scale in the token set, and inventing one here would put a second hue into
 * a deliberately achromatic system.
 */
const LABELS: Record<UsageCategory["category"], string> = {
  video: "Video",
  image: "Images",
  audio: "Audio",
  document: "Documents",
  other: "Other",
};

const SHADES = ["bg-neutral-400", "bg-neutral-500", "bg-neutral-600", "bg-neutral-700"];

export function StorageBreakdown({ usage }: { usage: UsageCategory[] }) {
  const total = usage.reduce((sum, u) => sum + u.bytes, 0);

  if (total === 0) {
    return <p className="text-muted-foreground text-xs">Nothing stored yet.</p>;
  }

  return (
    <div className="border-border flex flex-col gap-2 rounded-lg border p-3">
      <div className="flex items-baseline justify-between">
        <span className="text-xs tabular-nums">{fmtSize(total)} used</span>
        <span className="text-muted-foreground text-[10px] tabular-nums">
          {usage.reduce((n, u) => n + u.files, 0)} files
        </span>
      </div>

      <div className="bg-muted flex h-1.5 w-full overflow-hidden rounded-full">
        {usage.map((u, i) => (
          <div
            key={u.category}
            className={cn("h-full", i === 0 ? "bg-primary" : SHADES[(i - 1) % SHADES.length])}
            style={{ width: `${(u.bytes / total) * 100}%` }}
            title={`${LABELS[u.category]} — ${fmtSize(u.bytes)}`}
          />
        ))}
      </div>

      <ul className="flex flex-col gap-1">
        {usage.map((u, i) => (
          <li key={u.category} className="flex items-center gap-2 text-xs">
            <span
              aria-hidden
              className={cn(
                "size-2 shrink-0 rounded-[2px]",
                i === 0 ? "bg-primary" : SHADES[(i - 1) % SHADES.length],
              )}
            />
            <span className="truncate">{LABELS[u.category]}</span>
            <span className="text-muted-foreground ml-auto shrink-0 tabular-nums">
              {fmtSize(u.bytes)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
