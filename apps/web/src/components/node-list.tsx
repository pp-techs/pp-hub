import { memo, useMemo, useRef, useState, type MouseEvent } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
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
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import { canRestructure, type Node } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import {
  useNodeActions,
  type NodeAction,
  type NodeActionHandlers,
} from "@/components/node-actions";
import { NodeIcon } from "@/components/node-icon";
import { LoadingStatus } from "@/components/loading";
import { LISTING_PADDING } from "@/components/surface";
import { useUiStore } from "@/lib/ui-store";
import { DRAG_DATA_TYPE, isValidMoveTarget, setDragGhost } from "@/lib/drag-drop";

interface NodeListProps extends NodeActionHandlers {
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  nodes: Node[];
  /** Whether the caller may add to the folder these rows belong to. */
  canWriteHere: boolean;
  selectedIds: Set<string>;
  /** Modifier keys decide replace / toggle / range — the caller reads them. */
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
  onMoveNodes?: (nodes: Node[], targetId: string) => Promise<void>;
}

/**
 * The dense listing: 36px rows, right-aligned numeric columns and an actions
 * cell that only shows on hover or selection. A click selects the row — there
 * is no checkbox gutter, so selection reads as the row's own wash.
 */
export function NodeList({
  nodes,
  canWriteHere,
  selectedIds,
  starredIds,
  onClick,
  onDoubleClick,
  onMoveNodes,
  ...handlers
}: NodeListProps) {
  const selectedNodes = useMemo(() => {
    const map = new Map<string, Node>();
    for (const n of nodes) if (selectedIds.has(n.id)) map.set(n.id, n);
    return map;
  }, [nodes, selectedIds]);

  // Rows sit inside ContextMenuTrigger's `display: contents` wrapper, so
  // `divide-y` on the tbody never reaches them; each row draws its own border.
  return (
    <div className={LISTING_PADDING}>
      <table className="w-full border-collapse text-left text-xs">
        <NodeListHead />
        <tbody>
          {nodes.map((n) => (
            <NodeListRow
              key={n.id}
              node={n}
              selected={selectedIds.has(n.id)}
              starred={starredIds.has(n.id)}
              shared={n.shared ?? false}
              canWriteHere={canWriteHere}
              onClick={onClick}
              onDoubleClick={onDoubleClick}
              onMoveNodes={onMoveNodes}
              selectedNodes={selectedNodes}
              {...handlers}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Column labels, shared by the listing and its loading state so neither drifts. */
function NodeListHead() {
  return (
    <thead className="bg-background border-border text-muted-foreground sticky top-0 z-10 border-b text-[11px] tracking-wider uppercase">
      <tr className="h-9">
        <th scope="col" className="min-w-60 px-2 font-medium">
          <Trans>Name</Trans>
        </th>
        <th scope="col" className="w-36 px-2 text-right font-medium">
          <Trans>Modified</Trans>
        </th>
        <th scope="col" className="w-24 px-2 text-right font-medium">
          <Trans>Size</Trans>
        </th>
        <th scope="col" className="w-32 px-2 font-medium">
          <Trans>Owner</Trans>
        </th>
        <th scope="col" className="w-20 px-2 text-right font-medium">
          <Trans>Actions</Trans>
        </th>
      </tr>
    </thead>
  );
}

/**
 * The listing before its first answer: the real header over rows of the same
 * 36px height, so the data lands in place instead of pushing the page around.
 */
export function NodeListSkeleton({ rows = 8 }: { rows?: number }) {
  return (
    <LoadingStatus className={LISTING_PADDING}>
      <table className="w-full border-collapse text-left text-xs">
        <NodeListHead />
        <tbody>
          {Array.from({ length: rows }, (_, i) => (
            <tr key={i} className="border-border h-9 border-b">
              <td className="px-2">
                <div className="flex items-center gap-2">
                  <Skeleton className="size-4 rounded" />
                  <Skeleton className="h-3" style={{ width: `${40 + ((i * 17) % 35)}%` }} />
                </div>
              </td>
              <td className="px-2">
                <Skeleton className="ml-auto h-3 w-20" />
              </td>
              <td className="px-2">
                <Skeleton className="ml-auto h-3 w-12" />
              </td>
              <td className="px-2">
                <Skeleton className="h-3 w-20" />
              </td>
              <td className="px-2" />
            </tr>
          ))}
        </tbody>
      </table>
    </LoadingStatus>
  );
}

interface NodeListRowProps extends NodeActionHandlers {
  starred: boolean;
  shared: boolean;
  node: Node;
  canWriteHere: boolean;
  selected: boolean;
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
  onMoveNodes?: (nodes: Node[], targetId: string) => Promise<void>;
  selectedNodes: Map<string, Node>;
}

const NodeListRow = memo(function NodeListRow({
  node,
  canWriteHere,
  selected,
  starred,
  shared,
  onClick,
  onDoubleClick,
  onMoveNodes,
  selectedNodes,
  ...handlers
}: NodeListRowProps) {
  const openPanel = useUiStore((s) => s.openPanel);
  const { t, i18n } = useLingui();
  const f = useFormatters();
  const actions = useNodeActions(node, handlers, openPanel, starred, canWriteHere);
  // Named local so the extracted message reads `{nodeName}` rather than `{0}`.
  const nodeName = node.name;

  const canMove = canRestructure(node);
  const isDir = node.kind === "dir";
  const [isDropTarget, setIsDropTarget] = useState(false);
  const dragDepth = useRef(0);

  const handleDragStart = (e: React.DragEvent) => {
    if (!canMove) return;
    if ((e.target as HTMLElement)?.closest("button, [role='menuitem']")) {
      e.preventDefault();
      return;
    }
    const isSelected = selectedNodes.has(node.id);
    let nodesToDrag: Node[];
    if (isSelected) {
      const selectedList = [...selectedNodes.values()].filter(canRestructure);
      nodesToDrag = selectedList.length > 0 ? selectedList : [node];
    } else {
      nodesToDrag = [node];
    }
    e.dataTransfer.setData(DRAG_DATA_TYPE, JSON.stringify(nodesToDrag.map((n) => n.id)));
    // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer effectAllowed
    e.dataTransfer.effectAllowed = "move";
    setDragGhost(e, nodesToDrag);
    useUiStore.getState().setDraggingNodes({ spaceId: node.space_id, nodes: nodesToDrag });
  };

  const handleDragEnd = () => {
    useUiStore.getState().setDraggingNodes(null);
  };

  const handleDragEnter = (e: React.DragEvent) => {
    if (!isDir) return;
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (!isValidMoveTarget(node.id, dragging, node.space_id, storeDirs)) return;
    e.preventDefault();
    dragDepth.current += 1;
    setIsDropTarget(true);
  };

  const handleDragOver = (e: React.DragEvent) => {
    if (!isDir) return;
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (isValidMoveTarget(node.id, dragging, node.space_id, storeDirs)) {
      e.preventDefault();
      // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer dropEffect
      e.dataTransfer.dropEffect = "move";
    } else {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- dataTransfer dropEffect
      e.dataTransfer.dropEffect = "none";
    }
  };

  const handleDragLeave = () => {
    if (!isDir) return;
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) {
      setIsDropTarget(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    if (!isDir) return;
    dragDepth.current = 0;
    setIsDropTarget(false);
    const dragging = useUiStore.getState().draggingNodes;
    const storeDirs = useUiStore.getState().dirs;
    if (!isValidMoveTarget(node.id, dragging, node.space_id, storeDirs)) return;
    e.preventDefault();
    e.stopPropagation();
    if (dragging && onMoveNodes) {
      void onMoveNodes(dragging.nodes, node.id);
    }
    useUiStore.getState().setDraggingNodes(null);
  };
  return (
    <ContextMenu>
      {/* Stops the event reaching the listing's background trigger, whose menu
          acts on the folder: a right-click on a row is about the row, and two
          menus must never open at once. The row's own handler is on this same
          element, so it still runs. */}
      <ContextMenuTrigger className="contents" onContextMenu={(e) => e.stopPropagation()}>
        <tr
          data-node-id={node.id}
          draggable={canMove}
          onDragStart={handleDragStart}
          onDragEnd={handleDragEnd}
          onDragEnter={handleDragEnter}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          className={cn(
            "group border-border hover:bg-muted/40 h-9 cursor-pointer border-b transition-colors [content-visibility:auto] [contain-intrinsic-size:auto_36px] align-middle",
            // Soft wash rather than a border, to match the grid's cards.
            selected && "bg-primary/10 hover:bg-primary/15",
            isDropTarget && "bg-primary/15 ring-2 ring-primary ring-inset",
          )}
          onClick={(e) => onClick(node, e)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <td className="px-2 align-middle">
            <div className="flex min-w-0 items-center gap-2">
              <NodeIcon node={node} isShared={shared} />
              <span
                className="flex min-w-0 items-center gap-1.5 truncate font-medium"
                title={node.name}
              >
                <span className="truncate">{node.name}</span>
              </span>
            </div>
          </td>
          <td className="text-muted-foreground px-2 text-right tabular-nums align-middle">
            {node.modified_at ? f.date(node.modified_at) : "—"}
          </td>
          <td className="text-muted-foreground px-2 text-right tabular-nums align-middle">
            {node.size != null ? f.size(node.size) : "—"}
          </td>
          {/* Inside a team folder `owner` is only a byline — access comes from
              the folder's grants, not from whoever uploaded. Printing a name
              here would invite the reader to think that person controls the
              row, so the column names the holder instead. */}
          <td className="text-muted-foreground truncate px-2 align-middle">
            {node.in_team ? t`Team` : (node.owner ?? "—")}
          </td>
          <td className="px-2 text-right align-middle">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={t`Actions for ${nodeName}`}
                    className={cn(
                      "opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100",
                      selected && "opacity-100",
                    )}
                    onClick={(e) => e.stopPropagation()}
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                {actions.map((action) => (
                  <DropdownMenuItem
                    key={action.key}
                    variant={action.destructive ? "destructive" : undefined}
                    onClick={(e) => {
                      e.stopPropagation();
                      action.run();
                    }}
                  >
                    <action.icon /> {i18n._(action.label)}
                    {action.shortcut && (
                      <span className="text-muted-foreground ml-auto pl-4 text-[10px]">
                        {action.shortcut}
                      </span>
                    )}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </td>
        </tr>
      </ContextMenuTrigger>

      <ContextMenuContent className="w-56">
        {actions.map((action) => (
          <ContextMenuItemWithSeparator key={action.key} action={action} />
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
});

function ContextMenuItemWithSeparator({ action }: { action: NodeAction }) {
  const { i18n } = useLingui();
  return (
    <>
      {action.startsGroup && <ContextMenuSeparator />}
      <ContextMenuItem
        variant={action.destructive ? "destructive" : undefined}
        onClick={action.run}
      >
        <action.icon data-icon="inline-start" /> {i18n._(action.label)}
        {action.shortcut && (
          <span className="text-muted-foreground ml-auto pl-4 text-[10px]">{action.shortcut}</span>
        )}
      </ContextMenuItem>
    </>
  );
}
