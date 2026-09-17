import { useEffect } from "react";
import { Link, useSearch } from "@tanstack/react-router";
import { Archive, Clock, House, Star, Trash2, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { railRow } from "@/components/rail-item";
import { useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";

/**
 * The space's own surfaces, each a child route of `/drive/$spaceId`.
 *
 * All five used to be split between top-level routes and `?panel=` values,
 * which is what made Trash and Shared vanish from the rail on Home, Recent and
 * Starred: those routes had no `spaceId` to hang a panel on. Now every row is a
 * link under the same space, so the set never changes.
 */
const rows: {
  to:
    | "/drive/$spaceId/home"
    | "/drive/$spaceId/recent"
    | "/drive/$spaceId/starred"
    | "/drive/$spaceId/shared"
    | "/drive/$spaceId/trash";
  label: string;
  icon: LucideIcon;
}[] = [
  { to: "/drive/$spaceId/home", label: "Home", icon: House },
  { to: "/drive/$spaceId/recent", label: "Recent", icon: Clock },
  { to: "/drive/$spaceId/starred", label: "Starred", icon: Star },
  { to: "/drive/$spaceId/shared", label: "Shared with me", icon: Users },
  { to: "/drive/$spaceId/trash", label: "Trash", icon: Trash2 },
];

export function QuickAccess({ spaceId }: { spaceId: string }) {
  const space = useSpace(spaceId);
  const rememberDir = useUiStore((s) => s.rememberDir);
  const archiveId = space?.archive_root_id;
  // Link's default active match compares the whole current search object, so
  // it would never mark this row active once sort/order/view join `dir` in
  // the URL. Checked by hand instead, the same shape `FolderTree` uses.
  const { dir } = useSearch({ strict: false }) as { dir?: string };

  // The archive is an ordinary root-level folder (see permissions.md), browsed
  // through the same `?dir=` listing as everything else. Its name has to be in
  // the dirs cache before the header ever asks for it — the breadcrumb has no
  // other way to learn what a node it never listed is called.
  useEffect(() => {
    if (archiveId) rememberDir({ id: archiveId, name: "Archive", parent_id: undefined });
  }, [archiveId, rememberDir]);

  return (
    <div className="flex flex-col gap-0.5">
      {rows.map(({ to, label, icon: Icon }) => (
        <Link
          key={to}
          to={to}
          params={{ spaceId }}
          className={railRow(false)}
          activeProps={{ className: railRow(true) }}
        >
          <Icon className="text-muted-foreground size-3.5 shrink-0" />
          <span className="truncate">{label}</span>
        </Link>
      ))}

      {/* Only an admin holds the grant that opens the archive (permissions.md),
          so showing this row to anyone else would just be a link to a 403. */}
      {space?.is_admin && archiveId && (
        <Link
          to="/drive/$spaceId"
          params={{ spaceId }}
          search={{ dir: archiveId }}
          className={railRow(dir === archiveId)}
        >
          <Archive className="text-muted-foreground size-3.5 shrink-0" />
          <span className="truncate">Archive</span>
        </Link>
      )}
    </div>
  );
}
