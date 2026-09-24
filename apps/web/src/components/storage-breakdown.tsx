import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
import { type UsageCategory } from "@/lib/api";
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { cn } from "@workspace/ui/lib/utils";
import { useFormatters } from "@/lib/format";

/**
 * Storage split by MIME family, plus the two things files leave behind: the
 * older versions of live files, and everything in the trash. Every row is
 * bytes held against the space, so together they are its storage meter.
 *
 * The design calls for one cyan segment and the rest in neutral greys: the
 * accent marks the largest category so the eye lands on what is actually
 * filling the disk, and the others stay quiet. There is no categorical colour
 * scale in the token set, and inventing one here would put a second hue into
 * a deliberately achromatic system.
 *
 * Descriptors rather than strings: this is module scope, evaluated at import
 * time before any catalog is active, so rendered labels here would pin the
 * first language loaded. The `Record` annotation is what keeps a new MIME
 * family from silently shipping without a label.
 */
const LABELS: Record<UsageCategory["category"], MessageDescriptor> = {
  video: msg`Video`,
  image: msg`Images`,
  audio: msg`Audio`,
  document: msg`Documents`,
  other: msg`Other`,
  versions: msg`Older versions`,
  trash: msg`Trash`,
};

/**
 * The count beside a row whose `files` is not live files: stored older
 * versions, and trashed files. Neither belongs in the header's file count —
 * a file with ten versions is one file, and a trashed file is not in the
 * space any more — so they are counted on their own row instead.
 */
function SideCount({ usage }: { usage: UsageCategory }) {
  // A named local, so the extracted message reads `{files, plural, …}`.
  const { files } = usage;
  let count: React.ReactNode;
  switch (usage.category) {
    case "versions":
      count = <Plural value={files} one="# version" other="# versions" />;
      break;
    case "trash":
      count = <Plural value={files} one="# file" other="# files" />;
      break;
    default:
      return null;
  }
  return <span className="text-muted-foreground shrink-0 text-[10px] tabular-nums">{count}</span>;
}

// eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind class names
const SHADES = ["bg-neutral-400", "bg-neutral-500", "bg-neutral-600", "bg-neutral-700"];

export function StorageBreakdown({ usage }: { usage: UsageCategory[] }) {
  const { i18n, t } = useLingui();
  const f = useFormatters();
  const total = usage.reduce((sum, u) => sum + u.bytes, 0);

  if (total === 0) {
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Nothing stored yet.</Trans>
      </p>
    );
  }

  const totalSize = f.size(total);
  const files = usage.reduce(
    (n, u) => (u.category === "versions" || u.category === "trash" ? n : n + u.files),
    0,
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle className="tabular-nums">
          <Trans>{totalSize} used</Trans>
        </CardTitle>
        <CardAction className="text-muted-foreground text-[10px] tabular-nums">
          <Plural value={files} one="# file" other="# files" />
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <div className="bg-muted flex h-1.5 w-full overflow-hidden rounded-full">
          {usage.map((u, i) => {
            // Named locals so the extracted message reads `{label} — {size}`
            // rather than `{0} — {1}`.
            const label = i18n._(LABELS[u.category]);
            const size = f.size(u.bytes);
            return (
              <div
                key={u.category}
                className={cn("h-full", i === 0 ? "bg-primary" : SHADES[(i - 1) % SHADES.length])}
                style={{ width: `${(u.bytes / total) * 100}%` }}
                title={t`${label} — ${size}`}
              />
            );
          })}
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
              <span className="truncate">{i18n._(LABELS[u.category])}</span>
              <SideCount usage={u} />
              <span className="text-muted-foreground ml-auto shrink-0 tabular-nums">
                {f.size(u.bytes)}
              </span>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
