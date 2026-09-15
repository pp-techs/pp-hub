import { useState } from "react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@workspace/ui/components/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { cn } from "@workspace/ui/lib/utils";
import { fmtDate, fmtSize, previewKind, type Node } from "@/lib/api";
import { nodeActions, type NodeActionHandlers } from "@/components/node-actions";
import { NodeIcon } from "@/components/node-table";
import { useUiStore } from "@/lib/ui-store";

interface NodeGridProps extends NodeActionHandlers {
  dirs: Node[];
  files: Node[];
  selectedIds: Set<string>;
  focusedId?: string;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
}

/** Uppercase section heading with the count, as in the Stitch grid. */
function GroupLabel({ label, count }: { label: string; count: number }) {
  return (
    <div className="text-muted-foreground mb-2 flex items-center gap-1.5 text-[11px] font-medium tracking-wider uppercase">
      <span>{label}</span>
      <span className="text-border">·</span>
      <span className="tabular-nums">{count}</span>
    </div>
  );
}

export function NodeGrid({
  dirs,
  files,
  selectedIds,
  focusedId,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeGridProps) {
  return (
    <div className="flex flex-col gap-6 p-4">
      {dirs.length > 0 && (
        <section>
          <GroupLabel label="Folders" count={dirs.length} />
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {dirs.map((node) => (
              <NodeCard
                key={node.id}
                node={node}
                selected={selectedIds.has(node.id) || focusedId === node.id}
                onClick={onClick}
                onDoubleClick={onDoubleClick}
                {...handlers}
              />
            ))}
          </div>
        </section>
      )}

      {files.length > 0 && (
        <section>
          <GroupLabel label="Files" count={files.length} />
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {files.map((node) => (
              <NodeCard
                key={node.id}
                node={node}
                selected={selectedIds.has(node.id) || focusedId === node.id}
                onClick={onClick}
                onDoubleClick={onDoubleClick}
                {...handlers}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

/** Image files have a real server-rendered thumbnail; everything else gets its icon. */
function Thumb({ node }: { node: Node }) {
  const [failed, setFailed] = useState(false);
  const hasThumb = node.kind === "file" && previewKind(node.mime) === "thumbnail";

  if (hasThumb && !failed) {
    return (
      <img
        src={`/api/v1/nodes/${node.id}/preview?kind=thumbnail`}
        alt=""
        loading="lazy"
        className="size-full object-cover"
        onError={() => setFailed(true)}
      />
    );
  }

  return <NodeIcon node={node} className="text-muted-foreground size-8" />;
}

interface NodeCardProps extends NodeActionHandlers {
  node: Node;
  selected: boolean;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
}

function NodeCard({ node, selected, onClick, onDoubleClick, ...handlers }: NodeCardProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const actions = nodeActions(node, handlers, openPanel);
  const isDir = node.kind === "dir";

  const meta = isDir
    ? node.modified_at
      ? fmtDate(node.modified_at)
      : "Folder"
    : [
        node.size != null ? fmtSize(node.size) : null,
        node.modified_at ? fmtDate(node.modified_at) : null,
      ]
        .filter(Boolean)
        .join(" · ");

  return (
    <ContextMenu>
      <ContextMenuTrigger>
        <div
          className={cn(
            "group border-border hover:border-foreground/20 relative cursor-pointer rounded-md border transition-colors",
            selected && "border-primary ring-primary/30 ring-2",
          )}
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          {isDir ? (
            <div className="flex items-center gap-2 p-2.5">
              <NodeIcon node={node} className="text-muted-foreground size-4 shrink-0" />
              <div className="flex min-w-0 flex-col leading-tight">
                <span className="truncate text-xs font-medium" title={node.name}>
                  {node.name}
                </span>
                <span className="text-muted-foreground truncate text-[10px]">{meta}</span>
              </div>
            </div>
          ) : (
            <>
              <div className="bg-muted/50 flex aspect-[4/3] items-center justify-center overflow-hidden rounded-t-md">
                <Thumb node={node} />
              </div>
              <div className="flex flex-col gap-0.5 p-2 leading-tight">
                <span className="truncate text-xs font-medium" title={node.name}>
                  {node.name}
                </span>
                <span className="text-muted-foreground truncate text-[10px] tabular-nums">
                  {meta}
                </span>
              </div>
            </>
          )}

          <div className="absolute top-1 right-1">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Actions for ${node.name}`}
                    className={cn(
                      "bg-background/80 opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100 focus-visible:opacity-100",
                      selected && "opacity-100",
                    )}
                    onClick={(e) => e.stopPropagation()}
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {actions.map((action) => (
                  <DropdownMenuItem
                    key={action.key}
                    variant={action.destructive ? "destructive" : undefined}
                    onClick={(e) => {
                      e.stopPropagation();
                      action.run();
                    }}
                  >
                    <action.icon /> {action.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </ContextMenuTrigger>

      <ContextMenuContent>
        {actions.map((action) => (
          <span key={action.key} className="contents">
            {action.startsGroup && <ContextMenuSeparator />}
            <ContextMenuItem
              variant={action.destructive ? "destructive" : undefined}
              onClick={action.run}
            >
              <action.icon data-icon="inline-start" /> {action.label}
            </ContextMenuItem>
          </span>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
