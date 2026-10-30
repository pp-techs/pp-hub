import {
  Fragment,
  memo,
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent,
  type RefObject,
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Trans, useLingui } from "@lingui/react/macro";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@workspace/ui/components/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import { canRestructure, type Node } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { groupActions } from "@/lib/group-actions";
import { useNodeActions, type NodeActionHandlers } from "@/components/node-actions";
import { NodeIcon } from "@/components/node-icon";
import { ExtOffBadge } from "@/components/ext-off-badge";
import { SubPageCount } from "@/components/sub-page-count";
import { LoadingStatus } from "@/components/loading";
import { LISTING_PADDING } from "@/components/surface";
import { useUiStore } from "@/lib/ui-store";
import { DRAG_DATA_TYPE, isValidMoveTarget, setDragGhost } from "@/lib/drag-drop";
import { hitTestList } from "@/lib/listing-geometry";
import type { MarqueeRect } from "@/lib/use-marquee-selection";

/**
 * Which listing columns survive at which width, in the order Name, Modified,
 * Size, Owner, Actions. A phone fits the name and the row's action menu; the
 * numbers come back as the column grows. The header, the rows and the loading
 * skeleton all read this, so a column never shows a head without its cells.
 */
/* eslint-disable lingui/no-unlocalized-strings -- Tailwind class names */
const COL_VISIBLE = [
  "",
  "hidden sm:table-cell",
  "hidden md:table-cell",
  "hidden lg:table-cell",
  "",
] as const;
/* eslint-enable lingui/no-unlocalized-strings */

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
  /** The route's scroller: the virtualizer's scroll element. */
  scrollRef: RefObject<HTMLDivElement | null>;
  /** Receives this listing's marquee hit test, re-assigned every render. */
  hitTestRef: RefObject<((band: MarqueeRect) => Set<string>) | null>;
}

/**
 * The dense listing: 36px rows, right-aligned numeric columns and an actions
 * cell that only shows on hover or selection. A click selects the row — there
 * is no checkbox gutter, so selection reads as the row's own wash.
 *
 * Only the rows near the viewport are mounted: spacer rows above and below
 * stand in for the rest, so a folder of thousands costs what a screenful does.
 */
export function NodeList({
  nodes,
  canWriteHere,
  selectedIds,
  starredIds,
  onClick,
  onDoubleClick,
  onMoveNodes,
  scrollRef,
  hitTestRef,
  ...handlers
}: NodeListProps) {
  // Read at drag start rather than passed down as a fresh collection, so a
  // selection change re-renders only the rows whose `selected` flipped.
  const nodesRef = useRef(nodes);
  nodesRef.current = nodes;
  const selectedIdsRef = useRef(selectedIds);
  selectedIdsRef.current = selectedIds;
  const getSelectedNodes = useCallback(
    () => nodesRef.current.filter((n) => selectedIdsRef.current.has(n.id)),
    [],
  );

  const tbodyRef = useRef<HTMLTableSectionElement>(null);
  // Where the rows start inside the scroller's content: the padding and the
  // header above them.
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const tbody = tbodyRef.current;
    const scroller = scrollRef.current;
    if (!tbody || !scroller) return;
    setScrollMargin(
      tbody.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop,
    );
  }, []);
  // 36px is the `h-9` rows' height; the first mounted row confirms it.
  const [rowHeight, setRowHeight] = useState(36);

  const getItemKey = useCallback((i: number) => nodes[i]!.id, [nodes]);
  const virtualizer = useVirtualizer({
    count: nodes.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 12,
    scrollMargin,
    getItemKey,
  });
  const items = virtualizer.getVirtualItems();

  const firstKey = items[0]?.key;
  useLayoutEffect(() => {
    const row = tbodyRef.current?.querySelector<HTMLElement>("tr[data-node-id]");
    if (!row) return;
    const height = row.getBoundingClientRect().height;
    if (height > 0 && height !== rowHeight) setRowHeight(height);
  }, [firstKey, rowHeight]);
  useLayoutEffect(() => {
    virtualizer.measure();
  }, [rowHeight, virtualizer]);

  hitTestRef.current = (band) => {
    const tbody = tbodyRef.current;
    const scroller = scrollRef.current;
    if (!tbody || !scroller) return new Set();
    const box = tbody.getBoundingClientRect();
    const bounds = scroller.getBoundingClientRect();
    const top = box.top - bounds.top + scroller.scrollTop;
    const left = box.left - bounds.left + scroller.scrollLeft;
    return hitTestList(nodes, rowHeight, box.width, {
      ...band,
      left: band.left - left,
      top: band.top - top,
    });
  };

  const padTop = items.length > 0 ? items[0]!.start - scrollMargin : 0;
  const padBottom =
    items.length > 0 ? virtualizer.getTotalSize() - (items.at(-1)!.end - scrollMargin) : 0;

  // Rows sit inside ContextMenuTrigger's `display: contents` wrapper, so
  // `divide-y` on the tbody never reaches them; each row draws its own border.
  return (
    <div className={LISTING_PADDING}>
      <table className="w-full border-collapse text-left text-xs">
        <NodeListHead />
        <tbody ref={tbodyRef}>
          {padTop > 0 && (
            <tr aria-hidden>
              <td colSpan={5} style={{ height: padTop, padding: 0, border: 0 }} />
            </tr>
          )}
          {items.map((item) => {
            const n = nodes[item.index]!;
            return (
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
                getSelectedNodes={getSelectedNodes}
                {...handlers}
              />
            );
          })}
          {padBottom > 0 && (
            <tr aria-hidden>
              <td colSpan={5} style={{ height: padBottom, padding: 0, border: 0 }} />
            </tr>
          )}
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
        <th scope="col" className={cn("min-w-60 px-2 font-medium", COL_VISIBLE[0])}>
          <Trans>Name</Trans>
        </th>
        <th scope="col" className={cn("w-36 px-2 text-right font-medium", COL_VISIBLE[1])}>
          <Trans>Modified</Trans>
        </th>
        <th scope="col" className={cn("w-24 px-2 text-right font-medium", COL_VISIBLE[2])}>
          <Trans>Size</Trans>
        </th>
        <th scope="col" className={cn("w-32 px-2 font-medium", COL_VISIBLE[3])}>
          <Trans>Owner</Trans>
        </th>
        <th scope="col" className={cn("w-20 px-2 text-right font-medium", COL_VISIBLE[4])}>
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
              <td className={cn("px-2", COL_VISIBLE[0])}>
                <div className="flex items-center gap-2">
                  <Skeleton className="size-4 rounded" />
                  <Skeleton className="h-3" style={{ width: `${40 + ((i * 17) % 35)}%` }} />
                </div>
              </td>
              <td className={cn("px-2", COL_VISIBLE[1])}>
                <Skeleton className="ml-auto h-3 w-20" />
              </td>
              <td className={cn("px-2", COL_VISIBLE[2])}>
                <Skeleton className="ml-auto h-3 w-12" />
              </td>
              <td className={cn("px-2", COL_VISIBLE[3])}>
                <Skeleton className="h-3 w-20" />
              </td>
              <td className={cn("px-2", COL_VISIBLE[4])} />
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
  /** The selected rows, read when a drag starts. */
  getSelectedNodes: () => Node[];
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
  getSelectedNodes,
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
    const selectedList = selected ? getSelectedNodes().filter(canRestructure) : [];
    const nodesToDrag = selectedList.length > 0 ? selectedList : [node];
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
            "group border-border hover:bg-muted/40 h-9 cursor-pointer border-b transition-colors align-middle",
            // Soft wash rather than a border, to match the grid's cards.
            selected && "bg-primary/10 hover:bg-primary/15",
            isDropTarget && "bg-primary/15 ring-2 ring-primary ring-inset",
          )}
          onClick={(e) => onClick(node, e)}
          onDoubleClick={() => onDoubleClick(node)}
        >
          <td className={cn("px-2 align-middle", COL_VISIBLE[0])}>
            <div className="flex min-w-0 items-center gap-2">
              <NodeIcon node={node} isShared={shared} />
              <span
                className="flex min-w-0 items-center gap-1.5 truncate font-medium"
                title={node.name}
              >
                <span className="truncate">{node.name}</span>
                <SubPageCount node={node} />
                <ExtOffBadge node={node} />
              </span>
            </div>
          </td>
          <td
            className={cn(
              "text-muted-foreground px-2 text-right tabular-nums align-middle",
              COL_VISIBLE[1],
            )}
          >
            {node.modified_at ? f.date(node.modified_at) : "—"}
          </td>
          <td
            className={cn(
              "text-muted-foreground px-2 text-right tabular-nums align-middle",
              COL_VISIBLE[2],
            )}
          >
            {node.size != null ? f.size(node.size) : "—"}
          </td>
          {/* Inside a team folder `owner` is only a byline — access comes from
              the folder's grants, not from whoever uploaded. Printing a name
              here would invite the reader to think that person controls the
              row, so the column names the holder instead. */}
          <td className={cn("text-muted-foreground truncate px-2 align-middle", COL_VISIBLE[3])}>
            {node.in_team ? t`Team` : (node.owner ?? "—")}
          </td>
          <td className={cn("px-2 text-right align-middle", COL_VISIBLE[4])}>
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
                {groupActions(actions).map((group, i) => (
                  <Fragment key={group[0]!.key}>
                    {i > 0 && <DropdownMenuSeparator />}
                    <DropdownMenuGroup>
                      {group.map((action) => (
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
                            <DropdownMenuShortcut>{action.shortcut}</DropdownMenuShortcut>
                          )}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuGroup>
                  </Fragment>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </td>
        </tr>
      </ContextMenuTrigger>

      <ContextMenuContent className="w-56">
        {groupActions(actions).map((group, i) => (
          <Fragment key={group[0]!.key}>
            {i > 0 && <ContextMenuSeparator />}
            <ContextMenuGroup>
              {group.map((action) => (
                <ContextMenuItem
                  key={action.key}
                  variant={action.destructive ? "destructive" : undefined}
                  onClick={action.run}
                >
                  <action.icon data-icon="inline-start" /> {i18n._(action.label)}
                  {action.shortcut && <ContextMenuShortcut>{action.shortcut}</ContextMenuShortcut>}
                </ContextMenuItem>
              ))}
            </ContextMenuGroup>
          </Fragment>
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
});
