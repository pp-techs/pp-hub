import { memo, useState } from "react";
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
import { fmtDate, fmtSize, hasThumbnail, previewUrl, thumbnailFit, type Node } from "@/lib/api";
import { useNodeActions, type NodeActionHandlers } from "@/components/node-actions";
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
  const countSelected = (nodes: Node[]) => {
    let count = 0;
    for (const n of nodes) if (selectedIds.has(n.id)) count++;
    return count;
  };
  const card = (node: Node) => (
    <NodeCard
      key={node.id}
      node={node}
      selected={selectedIds.has(node.id)}
      starred={starredIds.has(node.id)}
      shared={node.shared ?? false}
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
 * The card body: the rendered thumbnail, or the file-type icon.
 *
 * Falling back to the icon rather than an empty frame matters most for the
 * cases that are not errors — a text file or an archive never gets a thumbnail
 * at all, and a PDF has none until the worker renders it (or at all, if no
 * worker is deployed). A blank card reads as broken in every one of those,
 * while the icon still says what the file is. Matches the details panel, which
 * has always fallen back this way.
 */
function Thumb({ node }: { node: Node }) {
  const [failed, setFailed] = useState(false);
  const hasThumb = hasThumbnail(node) && !failed;

  return (
    <div className="bg-background flex h-36 w-full items-center justify-center overflow-hidden rounded-xl">
      {hasThumb ? (
        <img
          src={previewUrl(node)}
          alt=""
          loading="lazy"
          // Pictures crop to fill, pages letterbox — see thumbnailFit. The
          // classes are spelled out because Tailwind cannot see a name built
          // at runtime.
          className={cn(
            "size-full",
            thumbnailFit(node) === "cover" ? "object-cover" : "object-contain",
          )}
          onError={() => setFailed(true)}
        />
      ) : (
        <NodeIcon node={node} className="size-12" />
      )}
    </div>
  );
}

interface NodeCardProps extends NodeActionHandlers {
  starred: boolean;
  shared: boolean;
  node: Node;
  /** Clicked — feeds the bulk action bar and the details panel. */
  selected: boolean;
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
}

const NodeCard = memo(function NodeCard({
  node,
  selected,
  starred,
  shared,
  onClick,
  onDoubleClick,
  ...handlers
}: NodeCardProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const actions = useNodeActions(node, handlers, openPanel, starred);
  const isDir = node.kind === "dir";

  // Size and date only live in the tooltip: the Stitch card carries the name
  // alone, and the details panel and list view are where numbers belong.
  const meta = isDir
    ? [node.modified_at ? fmtDate(node.modified_at) : "Folder"]
    : [
        node.size != null ? fmtSize(node.size) : null,
        node.modified_at ? fmtDate(node.modified_at) : null,
      ];
  const label = node.name;
  const title = [label, ...meta.filter(Boolean)].join("\n");

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
    <span className="flex min-w-0 items-center gap-1.5 truncate text-xs font-medium" title={title}>
      <span className="truncate">{label}</span>
    </span>
  );

  return (
    <ContextMenu>
      <ContextMenuTrigger>
        <div
          data-node-id={node.id}
          className={cn(
            "group bg-muted/60 hover:bg-muted cursor-pointer border border-transparent transition-colors [content-visibility:auto]",
            isDir
              ? "flex h-12 items-center justify-between rounded-xl px-3.5 [contain-intrinsic-size:auto_48px]"
              : "rounded-2xl p-2.5 [contain-intrinsic-size:auto_140px]",
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
                <NodeIcon node={node} isShared={shared} className="size-5" />
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
});
