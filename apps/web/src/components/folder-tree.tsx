import { Trans, useLingui } from "@lingui/react/macro";
import { useState } from "react";
import { Link, useSearch } from "@tanstack/react-router";
import { ChevronRight, Folder, FolderOpen, Loader2 } from "lucide-react";
import { useSpace, useTree } from "@/lib/queries";
import {
  SidebarMenuSkeleton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@workspace/ui/components/sidebar";
import { cn } from "@workspace/ui/lib/utils";
import type { TreeNode } from "@/lib/api";

interface FolderTreeItemProps {
  node: TreeNode;
  currentDirId: string | undefined;
  spaceId: string;
}

function FolderTreeItem({ node, currentDirId, spaceId }: FolderTreeItemProps) {
  const [expanded, setExpanded] = useState(false);
  const { t } = useLingui();
  // Keyed on this node's own id, gated on `expanded`: collapsed items must not
  // query at all. They used to call `useTree(spaceId, undefined)` while
  // collapsed — the same key as the space's root tree — so every rendered row
  // stayed subscribed to root data it never used, and the query cache carried
  // one extra live subscriber per folder in the rail for no reason.
  const { data: children, isLoading } = useTree(spaceId, node.id, expanded);

  const isActive = node.id === currentDirId;
  const dirs = children?.filter((n) => n.kind === "dir") ?? [];

  return (
    <SidebarMenuSubItem>
      <div className="flex items-center gap-0.5">
        <button
          type="button"
          aria-label={expanded ? t`Collapse` : t`Expand`}
          aria-expanded={expanded}
          className="text-sidebar-foreground/70 hover:text-sidebar-accent-foreground shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            setExpanded((v) => !v);
          }}
        >
          <ChevronRight
            className={cn(
              "size-3.5 transition-transform",
              expanded && "rotate-90",
              !node.has_children && "invisible",
            )}
          />
        </button>
        {/* A real link, not an onClick button: consistent with every other rail
            row, and it is what lets a middle-click or ⌘-click open the folder
            in a new tab like the rest of the app. */}
        <SidebarMenuSubButton
          isActive={isActive}
          className="min-w-0 flex-1"
          render={<Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: node.id }} />}
        >
          {expanded ? (
            <FolderOpen className={cn(isActive ? "text-primary! fill-primary/30" : "")} />
          ) : (
            <Folder className={cn(isActive ? "text-primary! fill-primary/30" : "")} />
          )}
          <span className="truncate">{node.name}</span>
        </SidebarMenuSubButton>
        {isLoading && <Loader2 className="text-muted-foreground size-3 shrink-0 animate-spin" />}
      </div>

      {expanded && dirs.length > 0 && (
        <SidebarMenuSub className="mx-0 mt-0.5 px-1.5">
          {dirs.map((child) => (
            <FolderTreeItem
              key={child.id}
              node={child}
              currentDirId={currentDirId}
              spaceId={spaceId}
            />
          ))}
        </SidebarMenuSub>
      )}
    </SidebarMenuSubItem>
  );
}

/**
 * The space's folder tree, rendered as the disclosure under My Drive. Returns
 * the `<ul>` itself so the caller only has to decide whether it is open.
 */
export function FolderTree({ spaceId }: { spaceId: string }) {
  // Read the open folder from the router rather than window.location: router
  // navigations use pushState, which never fires popstate.
  const { dir } = useSearch({ strict: false }) as { dir?: string };
  const space = useSpace(spaceId);
  const { data: roots, isLoading } = useTree(spaceId);

  if (isLoading) {
    return (
      <SidebarMenuSub>
        <SidebarMenuSkeleton />
        <SidebarMenuSkeleton />
      </SidebarMenuSub>
    );
  }

  // The archive has its own rail row; it must not also show up as an ordinary
  // expandable folder here, or it is mixed with real files twice.
  const rootDirs = roots?.filter((n) => n.kind === "dir" && n.id !== space?.archive_root_id) ?? [];

  if (rootDirs.length === 0) {
    return (
      <SidebarMenuSub>
        <li className="text-muted-foreground px-2 py-1 text-xs">
          <Trans>No folders yet.</Trans>
        </li>
      </SidebarMenuSub>
    );
  }

  return (
    <SidebarMenuSub>
      {rootDirs.map((node) => (
        <FolderTreeItem key={node.id} node={node} currentDirId={dir} spaceId={spaceId} />
      ))}
    </SidebarMenuSub>
  );
}
