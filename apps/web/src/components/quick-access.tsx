import { useEffect, useState } from "react";
import { Link, useLocation, useSearch } from "@tanstack/react-router";
import {
  Archive,
  ChevronRight,
  Clock,
  CloudUpload,
  FolderPlus,
  FolderUp,
  HardDrive,
  House,
  Plus,
  Star,
  Trash2,
  Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Button } from "@workspace/ui/components/button";
import { railRow } from "@/components/rail-item";
import { useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";

type Row = {
  to:
    | "/drive/$spaceId/home"
    | "/drive/$spaceId/recent"
    | "/drive/$spaceId/starred"
    | "/drive/$spaceId/shared"
    | "/drive/$spaceId/trash";
  label: string;
  icon: LucideIcon;
};

/**
 * The space's own surfaces, each a child route of `/drive/$spaceId`.
 *
 * All five used to be split between top-level routes and `?panel=` values,
 * which is what made Trash and Shared vanish from the rail on Home, Recent and
 * Starred: those routes had no `spaceId` to hang a panel on. Now every row is a
 * link under the same space, so the set never changes.
 */
const homeRow: Row = { to: "/drive/$spaceId/home", label: "Home", icon: House };
const smartRows: Row[] = [
  { to: "/drive/$spaceId/shared", label: "Shared with me", icon: Users },
  { to: "/drive/$spaceId/recent", label: "Recent", icon: Clock },
  { to: "/drive/$spaceId/starred", label: "Starred", icon: Star },
];
const cleanupRows: Row[] = [{ to: "/drive/$spaceId/trash", label: "Trash", icon: Trash2 }];

/** Spacer between rail groups — keeps the rhythm without labels. */
function SidebarSeparator() {
  return <div aria-hidden className="h-2" />;
}

export function QuickAccess({
  spaceId,
  onNewFolder,
  onUploadFiles,
  onUploadFolder,
  children,
}: {
  spaceId: string;
  onNewFolder: () => void;
  onUploadFiles: () => void;
  onUploadFolder: () => void;
  /** My Drive folder tree, rendered nested under the My Drive disclosure. */
  children?: React.ReactNode;
}) {
  const space = useSpace(spaceId);
  const rememberDir = useUiStore((s) => s.rememberDir);
  const archiveId = space?.archive_root_id;
  const [driveOpen, setDriveOpen] = useState(true);
  const location = useLocation();
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
      {/* 1. New button — big primary button */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              className="outline-none h-10 border-primary dark:border-primary text-primary hover:text-primary hover:bg-primary/20 dark:hover:bg-primary/30 mb-3"
              size="lg"
              variant="ghost"
            >
              <Plus />
              New
            </Button>
          }
        />
        <DropdownMenuContent align="start">
          <DropdownMenuItem onClick={onUploadFiles}>
            <CloudUpload /> Upload files
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onUploadFolder}>
            <FolderUp /> Upload folder
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onNewFolder}>
            <FolderPlus /> New folder
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* 2. Home */}
      <NavRow row={homeRow} spaceId={spaceId} />

      {/* 3. My Drive — the row navigates to the drive root; only the chevron
          toggles the disclosure. Active at the root listing, checked by hand
          for the same search-param reason as every other rail row. */}
      <div>
        <div className={railRow(location.pathname === `/drive/${spaceId}` && !dir)}>
          <Link
            to="/drive/$spaceId"
            params={{ spaceId }}
            search={{ dir: undefined }}
            className="flex min-w-0 flex-1 items-center gap-2 text-left"
          >
            <HardDrive className="text-muted-foreground size-3.5 shrink-0" />
            <span className="truncate">My Drive</span>
          </Link>
          <button
            type="button"
            aria-label={driveOpen ? "Collapse My Drive" : "Expand My Drive"}
            aria-expanded={driveOpen}
            className="text-muted-foreground hover:text-foreground shrink-0"
            onClick={() => setDriveOpen((v) => !v)}
          >
            <ChevronRight
              className={cn("size-3.5 transition-transform", driveOpen && "rotate-90")}
            />
          </button>
        </div>
        {driveOpen && <div className="ml-[18px] flex flex-col gap-0.5 pt-0.5">{children}</div>}
      </div>

      <SidebarSeparator />

      {/* 4. Smart views: Shared, Recent, Starred */}
      {smartRows.map((row) => (
        <NavRow key={row.label} row={row} spaceId={spaceId} />
      ))}

      <SidebarSeparator />

      {/* 5. Cleanup: Trash + Archive (admin only) */}
      {cleanupRows.map((row) => (
        <NavRow key={row.label} row={row} spaceId={spaceId} />
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

function NavRow({ row, spaceId }: { row: Row; spaceId: string }) {
  return (
    <Link
      to={row.to}
      params={{ spaceId }}
      className={railRow(false)}
      activeProps={{ className: railRow(true) }}
    >
      <row.icon className="text-muted-foreground size-3.5 shrink-0" />
      <span className="truncate">{row.label}</span>
    </Link>
  );
}
