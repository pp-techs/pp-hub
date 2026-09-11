import { useState, useEffect } from "react";
import { useParams, useNavigate } from "@tanstack/react-router";
import { ChevronRight, Folder } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { useTree } from "@/lib/queries";
import { cn } from "@workspace/ui/lib/utils";
import type { TreeNode } from "@/lib/api";

interface FolderTreeItemProps {
  node: TreeNode;
  depth: number;
  currentDirId: string | undefined;
  spaceId: string;
}

function FolderTreeItem({ node, depth, currentDirId, spaceId }: FolderTreeItemProps) {
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState(false);
  const { data: children, isLoading } = useTree(spaceId, expanded ? node.id : undefined);

  const isActive = node.id === currentDirId;
  const dirs = children?.filter((n) => n.kind === "dir") ?? [];

  return (
    <div>
      <Button
        variant="ghost"
        size="sm"
        className={cn(
          "h-7 w-full justify-start gap-1 px-2 py-0 text-xs",
          isActive && "bg-sidebar-accent font-medium",
        )}
        onClick={() => {
          if (node.kind === "dir") {
            setExpanded(!expanded);
          }
          if (spaceId) {
            void navigate({
              to: "/drive/$spaceId",
              params: { spaceId },
              search: { dir: node.id },
            });
          }
        }}
      >
        {node.kind === "dir" ? (
          <ChevronRight
            size={12}
            className={cn("shrink-0 transition-transform", expanded && "rotate-90")}
          />
        ) : (
          <span className="w-3 shrink-0" />
        )}
        <Folder size={12} className="shrink-0 text-muted-foreground" />
        <span className="truncate">{node.name}</span>
        {isLoading && (
          <span className="ml-auto shrink-0">
            <svg className="h-3 w-3 animate-spin" viewBox="0 0 24 24" fill="none">
              <circle
                className="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                strokeWidth="4"
              />
              <path
                className="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
              />
            </svg>
          </span>
        )}
      </Button>
      {expanded && dirs.length > 0 && (
        <div style={{ paddingLeft: `${(depth + 1) * 12}px` }}>
          {dirs.map((child) => (
            <FolderTreeItem
              key={child.id}
              node={child}
              depth={depth + 1}
              currentDirId={currentDirId}
              spaceId={spaceId}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function FolderTree() {
  const { spaceId } = useParams({ strict: false }) as { spaceId?: string };
  const [currentDirId, setCurrentDirId] = useState<string | undefined>(undefined);

  // Read dir from URL when component mounts and on popstate (back/forward navigation)
  useEffect(() => {
    const update = () => {
      const params = new URLSearchParams(window.location.search);
      setCurrentDirId(params.get("dir") || undefined);
    };
    update();
    window.addEventListener("popstate", update);
    return () => window.removeEventListener("popstate", update);
  }, []);

  if (!spaceId) {
    return null;
  }

  const { data: roots, isLoading } = useTree(spaceId);

  if (isLoading) {
    return <p className="px-3 py-2 text-xs text-muted-foreground">Loading…</p>;
  }

  const rootDirs = roots?.filter((n) => n.kind === "dir") ?? [];

  if (rootDirs.length === 0) {
    return <p className="px-3 py-2 text-xs text-muted-foreground">No folders yet.</p>;
  }

  return (
    <div className="flex flex-col gap-0.5 py-1">
      {rootDirs.map((node) => (
        <FolderTreeItem
          key={node.id}
          node={node}
          depth={0}
          currentDirId={currentDirId}
          spaceId={spaceId}
        />
      ))}
    </div>
  );
}
