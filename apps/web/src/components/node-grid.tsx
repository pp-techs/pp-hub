import { useState } from "react";
import type { MouseEvent } from "react";
import { MoreVertical } from "lucide-react";
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
import { NodeIcon } from "@/components/node-icon";
import { useUiStore } from "@/lib/ui-store";

/**
 * Column count tracks the width of the grid itself, not the viewport, so
 * opening the details panel drops 5 columns to 4 without the route having to
 * tell the grid anything.
 */
const GRID = "grid grid-cols-2 gap-3 @lg:grid-cols-3 @2xl:grid-cols-4 @4xl:grid-cols-5";

interface NodeGridProps extends NodeActionHandlers {
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  dirs: Node[];
  files: Node[];
  selectedIds: Set<string>;
  /** Modifier keys decide replace / toggle / range — the caller reads them. */
  onClick: (node: Node, event: MouseEvent) => void;
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
  onClick,
  onDoubleClick,
  ...handlers
}: NodeGridProps) {
  const countSelected = (nodes: Node[]) => nodes.filter((n) => selectedIds.has(n.id)).length;
  const card = (node: Node) => (
    <NodeCard
      key={node.id}
      node={node}
      selected={selectedIds.has(node.id)}
      starred={starredIds.has(node.id)}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      {...handlers}
    />
  );

  return (
    <div className="@container flex flex-col gap-6 p-4">
      {dirs.length > 0 && (
        <section>
          <GroupLabel label="Folders" count={dirs.length} selected={countSelected(dirs)} />
          <div className={GRID}>{dirs.map(card)}</div>
        </section>
      )}

      {files.length > 0 && (
        <section>
          <GroupLabel label="Files" count={files.length} selected={countSelected(files)} />
          <div className={GRID}>{files.map(card)}</div>
        </section>
      )}
    </div>
  );
}

/**
 * The card body. Every file shows its thumbnail; a file the preview worker
 * never rendered one for (anything but a raster image) shows the empty frame
 * rather than a stand-in, so the wall of cards stays quiet.
 */
function Thumb({ node }: { node: Node }) {
  const [failed, setFailed] = useState(false);
  const hasThumb = previewKind(node.mime) === "thumbnail" && !failed;

  return (
    <div className="bg-background border-border h-36 w-full overflow-hidden rounded-xl border">
      {hasThumb && (
        <img
          src={`/api/v1/nodes/${node.id}/preview?kind=thumbnail`}
          alt=""
          loading="lazy"
          className="size-full object-cover"
          onError={() => setFailed(true)}
        />
      )}
    </div>
  );
}

interface NodeCardProps extends NodeActionHandlers {
  starred: boolean;
  node: Node;
  /** Clicked — feeds the bulk action bar and the details panel. */
  selected: boolean;
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
}

function NodeCard({ node, selected, starred, onClick, onDoubleClick, ...handlers }: NodeCardProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const actions = nodeActions(node, handlers, openPanel, starred);
  const isDir = node.kind === "dir";

  // Size and date only live in the tooltip: the Stitch card carries the name
  // alone, and the details panel and list view are where numbers belong.
  const meta = isDir
    ? [node.modified_at ? fmtDate(node.modified_at) : "Folder"]
    : [
        node.size != null ? fmtSize(node.size) : null,
        node.modified_at ? fmtDate(node.modified_at) : null,
      ];
  const title = [node.name, ...meta.filter(Boolean)].join("\n");

  const kebab = (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Actions for ${node.name}`}
            className="text-muted-foreground hover:text-foreground size-6 shrink-0 rounded-full"
            onClick={(e) => e.stopPropagation()}
          />
        }
      >
        <MoreVertical />
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
  );

  const name = (
    <span className="truncate text-xs font-medium" title={title}>
      {node.name}
    </span>
  );

  return (
    <ContextMenu>
      <ContextMenuTrigger>
        <div
          data-node-id={node.id}
          className={cn(
            "group bg-muted/60 hover:bg-muted cursor-pointer border border-transparent transition-colors",
            isDir
              ? "flex h-12 items-center justify-between rounded-xl px-3.5"
              : "rounded-2xl p-2.5",
            // Selection is a soft wash, not an outline: a whole folder of
            // ticked cards should still read as a grid.
            selected && "bg-primary/10 hover:bg-primary/15 border-primary/30",
          )}
          onClick={(e) => onClick(node, e)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          {isDir ? (
            <>
              <div className="flex min-w-0 items-center gap-2.5">
                <NodeIcon node={node} className="size-5" />
                {name}
              </div>
              {kebab}
            </>
          ) : (
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-1 px-1">
                <div className="flex min-w-0 items-center gap-2">
                  <NodeIcon node={node} />
                  {name}
                </div>
                {kebab}
              </div>
              <Thumb node={node} />
            </div>
          )}
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
