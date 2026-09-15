import { useState } from "react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
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
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  dirs: Node[];
  files: Node[];
  selectedIds: Set<string>;
  focusedId?: string;
  onToggle: (node: Node, checked: boolean) => void;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
}

/** Uppercase section heading with the count, as in the Stitch grid. */
function GroupLabel({
  label,
  count,
  selected,
}: {
  label: string;
  count: number;
  selected: number;
}) {
  return (
    <div className="text-muted-foreground mb-2 flex items-center gap-1.5 text-[11px] font-medium tracking-wider uppercase">
      <span>{label}</span>
      <span className="text-border">·</span>
      <span className="tabular-nums">{count}</span>
      {selected > 0 && (
        <span className="text-primary tabular-nums normal-case">{selected} selected</span>
      )}
    </div>
  );
}

export function NodeGrid({
  dirs,
  files,
  selectedIds,
  starredIds,
  focusedId,
  onToggle,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeGridProps) {
  const countSelected = (nodes: Node[]) => nodes.filter((n) => selectedIds.has(n.id)).length;
  return (
    <div className="flex flex-col gap-6 p-4">
      {dirs.length > 0 && (
        <section>
          <GroupLabel label="Folders" count={dirs.length} selected={countSelected(dirs)} />
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {dirs.map((node) => (
              <NodeCard
                key={node.id}
                node={node}
                selected={selectedIds.has(node.id)}
                focused={focusedId === node.id}
                starred={starredIds.has(node.id)}
                onToggle={onToggle}
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
          <GroupLabel label="Files" count={files.length} selected={countSelected(files)} />
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {files.map((node) => (
              <NodeCard
                key={node.id}
                node={node}
                selected={selectedIds.has(node.id)}
                focused={focusedId === node.id}
                starred={starredIds.has(node.id)}
                onToggle={onToggle}
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

/**
 * The card's selection tick. Hidden until the card is hovered or already
 * selected, so a wall of thumbnails is not a wall of checkboxes.
 */
function SelectBox({
  node,
  selected,
  onToggle,
  className,
}: {
  node: Node;
  selected: boolean;
  onToggle: (node: Node, checked: boolean) => void;
  className?: string;
}) {
  return (
    <Checkbox
      checked={selected}
      onCheckedChange={(checked) => onToggle(node, checked === true)}
      // The card behind this reacts to both, and neither should also focus
      // the row or open the folder.
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      aria-label={`Select ${node.name}`}
      className={cn(
        "size-3.5 transition-opacity",
        selected ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
        className,
      )}
    />
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
  starred: boolean;
  node: Node;
  /** Ticked via its checkbox — feeds the bulk action bar. */
  selected: boolean;
  /** Last single-clicked — feeds the details panel. Not the same thing. */
  focused: boolean;
  onToggle: (node: Node, checked: boolean) => void;
  onClick: (node: Node) => void;
  onDoubleClick: (node: Node) => void;
}

function NodeCard({
  node,
  selected,
  focused,
  starred,
  onToggle,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeCardProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const actions = nodeActions(node, handlers, openPanel, starred);
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
            focused && !selected && "border-foreground/30 bg-muted/40",
            selected && "border-primary ring-primary/30 ring-2",
          )}
          onClick={() => onClick(node)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          {isDir ? (
            <div className="flex items-center gap-2 p-2.5">
              <span className="relative grid size-4 shrink-0 place-content-center">
                <NodeIcon
                  node={node}
                  className={cn(
                    "text-muted-foreground size-4 transition-opacity",
                    selected ? "opacity-0" : "opacity-100 group-hover:opacity-0",
                  )}
                />
                <SelectBox
                  node={node}
                  selected={selected}
                  onToggle={onToggle}
                  className="absolute inset-0 m-auto"
                />
              </span>
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

          {!isDir && (
            <SelectBox
              node={node}
              selected={selected}
              onToggle={onToggle}
              className="bg-background/80 absolute top-1 left-1 backdrop-blur-sm"
            />
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
                    {action.shortcut && (
                      <span className="text-muted-foreground ml-auto pl-4 text-[10px]">
                        {action.shortcut}
                      </span>
                    )}
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
              {action.shortcut && (
                <span className="text-muted-foreground ml-auto pl-4 text-[10px]">
                  {action.shortcut}
                </span>
              )}
            </ContextMenuItem>
          </span>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
}
