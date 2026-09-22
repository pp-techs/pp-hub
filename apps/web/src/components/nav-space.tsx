import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useSearch } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
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
import { useMoveNodes, useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { isValidMoveTarget } from "@/lib/drag-drop";

type Row = {
  to:
    | "/drive/$spaceId/home"
    | "/drive/$spaceId/shared"
    | "/drive/$spaceId/recent"
    | "/drive/$spaceId/starred"
    | "/drive/$spaceId/trash";
  label: MessageDescriptor;
  icon: LucideIcon;
};

/**
 * The space's own surfaces, each a child route of `/drive/$spaceId`. Grouped
 * the way they are used rather than alphabetically: where you start, what the
 * space collects for you, and what you are throwing away.
 *
 * The labels are descriptors, not rendered strings: these arrays are module
 * scope and are built once at import time, before a catalog is active, so a
 * rendered label here would freeze the first language loaded. `NavRow`
 * resolves them per render instead.
 */
const homeRow: Row = { to: "/drive/$spaceId/home", label: msg`Home`, icon: House };
const smartRows: Row[] = [
  { to: "/drive/$spaceId/shared", label: msg`Shared with me`, icon: Users },
  { to: "/drive/$spaceId/recent", label: msg`Recent`, icon: Clock },
  { to: "/drive/$spaceId/starred", label: msg`Starred`, icon: Star },
];
const cleanupRows: Row[] = [{ to: "/drive/$spaceId/trash", label: msg`Trash`, icon: Trash2 }];

/**
 * Every navigable row in the rail, on the Sidebar primitives so collapse, the
 * mobile sheet and the active wash all come from one place.
 */
export function NavSpace({ spaceId }: { spaceId: string }) {
  const space = useSpace(spaceId);
  const { t } = useLingui();
  const rememberDir = useUiStore((s) => s.rememberDir);
  const archiveId = space?.archive_root_id;
  const [driveOpen, setDriveOpen] = useState(true);
  const location = useLocation();
  // Link's default active match compares the whole current search object, so
  // it would never mark a row active once sort/order/view join `dir` in the
  // URL. Checked by hand instead, the same shape `FolderTree` uses.
  const { dir } = useSearch({ strict: false }) as { dir?: string };
  const atRoot = location.pathname === `/drive/${spaceId}`;
  const { moveNodes } = useMoveNodes(spaceId);
  const [isDropTarget, setIsDropTarget] = useState(false);
  const dragDepth = useRef(0);

  const handleDragEnter = (e: React.DragEvent) => {
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (!isValidMoveTarget(undefined, dragging, spaceId, storeDirs)) return;
    e.preventDefault();
    dragDepth.current += 1;
    setIsDropTarget(true);
  };

  const handleDragOver = (e: React.DragEvent) => {
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (isValidMoveTarget(undefined, dragging, spaceId, storeDirs)) {
      e.preventDefault();
      // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer dropEffect
      e.dataTransfer.dropEffect = "move";
    } else {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer dropEffect
      e.dataTransfer.dropEffect = "none";
    }
  };

  const handleDragLeave = () => {
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) {
      setIsDropTarget(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    dragDepth.current = 0;
    setIsDropTarget(false);
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (!isValidMoveTarget(undefined, dragging, spaceId, storeDirs)) return;
    e.preventDefault();
    e.stopPropagation();
    if (dragging) void moveNodes(dragging.nodes, undefined);
    useUiStore.getState().setDraggingNodes(null);
  };

  // The archive is an ordinary root-level folder (see permissions.md), browsed
  // through the same `?dir=` listing as everything else. Its name has to be in
  // the dirs cache before the header ever asks for it — the breadcrumb has no
  // other way to learn what a node it never listed is called.
  //
  // `t` changes identity when the catalog does, so the cached name is
  // rewritten on a language switch rather than keeping whichever language was
  // active the first time the space loaded.
  useEffect(() => {
    if (archiveId) rememberDir({ id: archiveId, name: t`Archive`, parent_id: undefined });
  }, [archiveId, rememberDir, t]);

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
                className={cn(
                  isDropTarget &&
                    "bg-sidebar-accent text-sidebar-accent-foreground ring-1 ring-primary",
                )}
                onDragEnter={handleDragEnter}
                onDragOver={handleDragOver}
                onDragLeave={handleDragLeave}
                onDrop={handleDrop}
                render={
                  <Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: undefined }} />
                }
              >
                <HardDrive />
                <span>
                  <Trans>My Drive</Trans>
                </span>
              </SidebarMenuButton>
              <SidebarMenuAction
                aria-label={driveOpen ? t`Collapse My Drive` : t`Expand My Drive`}
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
              <NavRow key={row.to} row={row} spaceId={spaceId} />
            ))}
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>

      <SidebarGroup className="mt-auto">
        <SidebarGroupContent>
          <SidebarMenu>
            {cleanupRows.map((row) => (
              <NavRow key={row.to} row={row} spaceId={spaceId} />
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
                  <span>
                    <Trans>Archive</Trans>
                  </span>
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
  const { i18n } = useLingui();
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        // eslint-disable-next-line lingui/no-unlocalized-strings -- route param token
        isActive={location.pathname === row.to.replace("$spaceId", spaceId)}
        render={<Link to={row.to} params={{ spaceId }} />}
      >
        <row.icon />
        <span>{i18n._(row.label)}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}
