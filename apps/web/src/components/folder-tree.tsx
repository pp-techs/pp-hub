import { useState } from "react";
import { Link, useSearch } from "@tanstack/react-router";
import { ChevronRight, Folder, Loader2 } from "lucide-react";
import { useSpace, useTree } from "@/lib/queries";
import { railRow } from "@/components/rail-item";
import { cn } from "@workspace/ui/lib/utils";
import type { TreeNode } from "@/lib/api";

interface FolderTreeItemProps {
  node: TreeNode;
  currentDirId: string | undefined;
  spaceId: string;
}

function FolderTreeItem({ node, currentDirId, spaceId }: FolderTreeItemProps) {
  const [expanded, setExpanded] = useState(false);
  // Keyed on this node's own id, gated on `expanded`: collapsed items must not
  // query at all. They used to call `useTree(spaceId, undefined)` while
  // collapsed — the same key as the space's root tree — so every rendered row
  // stayed subscribed to root data it never used, and the query cache carried
  // one extra live subscriber per folder in the rail for no reason.
  const { data: children, isLoading } = useTree(spaceId, node.id, expanded);

  const isActive = node.id === currentDirId;
  const dirs = children?.filter((n) => n.kind === "dir") ?? [];

  return (
    <div>
      <div className={railRow(isActive, "cursor-pointer gap-1.5")}>
        <button
          type="button"
          aria-label={expanded ? "Collapse" : "Expand"}
          aria-expanded={expanded}
          className="text-muted-foreground hover:text-foreground -ml-0.5 shrink-0"
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
        <Link
          to="/drive/$spaceId"
          params={{ spaceId }}
          search={{ dir: node.id }}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
        >
          <Folder
            className={cn(
              "size-3.5 shrink-0",
              isActive ? "fill-primary text-primary" : "text-muted-foreground",
            )}
          />
          <span className="truncate">{node.name}</span>
        </Link>
        {isLoading && <Loader2 className="text-muted-foreground size-3 shrink-0 animate-spin" />}
      </div>

      {expanded && dirs.length > 0 && (
        <div className="mt-0.5 flex flex-col gap-0.5 pl-4">
          {dirs.map((child) => (
            <FolderTreeItem
              key={child.id}
              node={child}
              currentDirId={currentDirId}
              spaceId={spaceId}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function FolderTree({ spaceId }: { spaceId: string }) {
  // Read the open folder from the router rather than window.location: router
  // navigations use pushState, which never fires popstate.
  const { dir } = useSearch({ strict: false }) as { dir?: string };
  const space = useSpace(spaceId);
  const { data: roots, isLoading } = useTree(spaceId);

  if (isLoading) {
    return <p className="text-muted-foreground px-2 py-1 text-xs">Loading…</p>;
  }

  // The archive has its own Quick Access row; it must not also show up as an
  // ordinary expandable folder here, or it is mixed with real files twice.
  const rootDirs = roots?.filter((n) => n.kind === "dir" && n.id !== space?.archive_root_id) ?? [];

  if (rootDirs.length === 0) {
    return <p className="text-muted-foreground px-2 py-1 text-xs">No folders yet.</p>;
  }

  return (
    <div className="flex flex-col gap-0.5">
      {rootDirs.map((node) => (
        <FolderTreeItem key={node.id} node={node} currentDirId={dir} spaceId={spaceId} />
      ))}
    </div>
  );
}
