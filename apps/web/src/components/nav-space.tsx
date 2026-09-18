import { useEffect, useState } from "react";
import { Link, useLocation, useSearch } from "@tanstack/react-router";
import { Archive, ChevronRight, Clock, HardDrive, House, Star, Trash2, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@workspace/ui/components/sidebar";
import { FolderTree } from "@/components/folder-tree";
import { useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";

type Row = {
  to:
    | "/drive/$spaceId/home"
    | "/drive/$spaceId/shared"
    | "/drive/$spaceId/recent"
    | "/drive/$spaceId/starred"
    | "/drive/$spaceId/trash";
  label: string;
  icon: LucideIcon;
};

/**
 * The space's own surfaces, each a child route of `/drive/$spaceId`. Grouped
 * the way they are used rather than alphabetically: where you start, what the
 * space collects for you, and what you are throwing away.
 */
const homeRow: Row = { to: "/drive/$spaceId/home", label: "Home", icon: House };
const smartRows: Row[] = [
  { to: "/drive/$spaceId/shared", label: "Shared with me", icon: Users },
  { to: "/drive/$spaceId/recent", label: "Recent", icon: Clock },
  { to: "/drive/$spaceId/starred", label: "Starred", icon: Star },
];
const cleanupRows: Row[] = [{ to: "/drive/$spaceId/trash", label: "Trash", icon: Trash2 }];

/**
 * Every navigable row in the rail, on the Sidebar primitives so collapse, the
 * mobile sheet and the active wash all come from one place.
 */
export function NavSpace({ spaceId }: { spaceId: string }) {
  const space = useSpace(spaceId);
  const rememberDir = useUiStore((s) => s.rememberDir);
  const archiveId = space?.archive_root_id;
  const [driveOpen, setDriveOpen] = useState(true);
  const location = useLocation();
  // Link's default active match compares the whole current search object, so
  // it would never mark a row active once sort/order/view join `dir` in the
  // URL. Checked by hand instead, the same shape `FolderTree` uses.
  const { dir } = useSearch({ strict: false }) as { dir?: string };
  const atRoot = location.pathname === `/drive/${spaceId}`;

  // The archive is an ordinary root-level folder (see permissions.md), browsed
  // through the same `?dir=` listing as everything else. Its name has to be in
  // the dirs cache before the header ever asks for it — the breadcrumb has no
  // other way to learn what a node it never listed is called.
  useEffect(() => {
    if (archiveId) rememberDir({ id: archiveId, name: "Archive", parent_id: undefined });
  }, [archiveId, rememberDir]);

  return (
    <>
      <SidebarGroup>
        <SidebarGroupContent>
          <SidebarMenu>
            <NavRow row={homeRow} spaceId={spaceId} />

            {/* My Drive — the row navigates to the drive root; only the action
                toggles the disclosure. Active at the root listing with no
                `?dir=`, checked by hand for the same reason as every row. */}
            <SidebarMenuItem>
              <SidebarMenuButton
                isActive={atRoot && !dir}
                render={
                  <Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: undefined }} />
                }
              >
                <HardDrive />
                <span>My Drive</span>
              </SidebarMenuButton>
              <SidebarMenuAction
                aria-label={driveOpen ? "Collapse My Drive" : "Expand My Drive"}
                aria-expanded={driveOpen}
                onClick={() => setDriveOpen((v) => !v)}
              >
                <ChevronRight className={cn("transition-transform", driveOpen && "rotate-90")} />
              </SidebarMenuAction>
              {driveOpen && <FolderTree spaceId={spaceId} />}
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>

      <SidebarGroup>
        <SidebarGroupContent>
          <SidebarMenu>
            {smartRows.map((row) => (
              <NavRow key={row.label} row={row} spaceId={spaceId} />
            ))}
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>

      <SidebarGroup className="mt-auto">
        <SidebarGroupContent>
          <SidebarMenu>
            {cleanupRows.map((row) => (
              <NavRow key={row.label} row={row} spaceId={spaceId} />
            ))}

            {/* Only an admin holds the grant that opens the archive
                (permissions.md), so showing this row to anyone else would just
                be a link to a 403. */}
            {space?.is_admin && archiveId && (
              <SidebarMenuItem>
                <SidebarMenuButton
                  isActive={dir === archiveId}
                  render={
                    <Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: archiveId }} />
                  }
                >
                  <Archive />
                  <span>Archive</span>
                </SidebarMenuButton>
              </SidebarMenuItem>
            )}
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
    </>
  );
}

function NavRow({ row, spaceId }: { row: Row; spaceId: string }) {
  const location = useLocation();
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={location.pathname === row.to.replace("$spaceId", spaceId)}
        render={<Link to={row.to} params={{ spaceId }} />}
      >
        <row.icon />
        <span>{row.label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
