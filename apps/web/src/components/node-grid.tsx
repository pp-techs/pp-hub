import {
  Fragment,
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import type { MouseEvent } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Plural, useLingui } from "@lingui/react/macro";
import { MoreVertical } from "lucide-react";
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
import {
  canRestructure,
  capsOf,
  hasThumbnail,
  isMarkdownFile,
  previewUrl,
  thumbnailFit,
  type Node,
} from "@/lib/api";
import { MarkdownThumbnail } from "@/components/markdown-document-view";
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
import { buildGridRows, gridColumns, hitTestGrid } from "@/lib/listing-geometry";
import type { MarqueeRect } from "@/lib/use-marquee-selection";

/**
 * The skeleton's columns track the width of the grid itself, not the
 * viewport, the same breakpoints `gridColumns` computes for the real grid.
 */
// eslint-disable-next-line lingui/no-unlocalized-strings -- Tailwind class names
const GRID = "grid grid-cols-2 gap-3 @lg:grid-cols-3 @2xl:grid-cols-4 @4xl:grid-cols-5";

interface NodeGridProps extends NodeActionHandlers {
  /** Node ids the caller has starred, so each row can show its own state. */
  starredIds: Set<string>;
  dirs: Node[];
  /** Whether the caller may add to the folder these cards belong to. */
  canWriteHere: boolean;
  files: Node[];
  selectedIds: Set<string>;
  /** Modifier keys decide replace / toggle / range — the caller reads them. */
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
  onMoveNodes?: (nodes: Node[], targetId: string) => Promise<void>;
  /** The route's scroller: the virtualizer's scroll element. */
  scrollRef: RefObject<HTMLDivElement | null>;
  /** Receives this grid's marquee hit test, re-assigned every render. */
  hitTestRef: RefObject<((band: MarqueeRect) => Set<string>) | null>;
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
    <div className="text-muted-foreground mb-2 flex h-4 items-center gap-1.5 text-[11px] font-medium tracking-wider uppercase">
      <span>{label}</span>
      <span className="text-border">·</span>
      <span className="tabular-nums">{count}</span>
      {selected > 0 && (
        <span className="text-primary tabular-nums normal-case">
          <Plural value={selected} one="# selected" other="# selected" />
        </span>
      )}
    </div>
  );
}

/**
 * Folders then files, in as many columns as the grid's own width allows.
 * Only the rows near the viewport are mounted, each placed from the computed
 * layout, so a folder of thousands costs what a screenful does.
 */
export function NodeGrid({
  dirs,
  canWriteHere,
  files,
  selectedIds,
  starredIds,
  onClick,
  onDoubleClick,
  onMoveNodes,
  scrollRef,
  hitTestRef,
  ...handlers
}: NodeGridProps) {
  const { t } = useLingui();
  const countSelected = (nodes: Node[]) => {
    let count = 0;
    for (const n of nodes) if (selectedIds.has(n.id)) count++;
    return count;
  };

  // Read at drag start rather than passed down as a fresh collection, so a
  // selection change re-renders only the cards whose `selected` flipped.
  const nodesRef = useRef<Node[]>([]);
  nodesRef.current = [...dirs, ...files];
  const selectedIdsRef = useRef(selectedIds);
  selectedIdsRef.current = selectedIds;
  const getSelectedNodes = useCallback(
    () => nodesRef.current.filter((n) => selectedIdsRef.current.has(n.id)),
    [],
  );

  const innerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  // Where the rows start inside the scroller's content: the padding above them.
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const inner = innerRef.current;
    if (!inner) return;
    const measure = () => {
      const scroller = scrollRef.current;
      setWidth(inner.clientWidth);
      if (scroller)
        setScrollMargin(
          inner.getBoundingClientRect().top -
            scroller.getBoundingClientRect().top +
            scroller.scrollTop,
        );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(inner);
    return () => observer.disconnect();
  }, [scrollRef]);

  const cols = gridColumns(width);
  const layout = useMemo(() => buildGridRows(dirs, files, cols), [dirs, files, cols]);
  const virtualizer = useVirtualizer({
    count: layout.rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: (i) => layout.rows[i]!.height,
    overscan: 3,
    scrollMargin,
    getItemKey: (i) => layout.rows[i]!.key,
  });
  useLayoutEffect(() => {
    virtualizer.measure();
  }, [layout, virtualizer]);

  hitTestRef.current = (band) => {
    const inner = innerRef.current;
    const scroller = scrollRef.current;
    if (!inner || !scroller) return new Set();
    const box = inner.getBoundingClientRect();
    const bounds = scroller.getBoundingClientRect();
    const top = box.top - bounds.top + scroller.scrollTop;
    const left = box.left - bounds.left + scroller.scrollLeft;
    return hitTestGrid(layout, cols, width, {
      ...band,
      left: band.left - left,
      top: band.top - top,
    });
  };

  const card = (node: Node) => (
    <NodeCard
      key={node.id}
      node={node}
      selected={selectedIds.has(node.id)}
      starred={starredIds.has(node.id)}
      shared={node.shared ?? false}
      canWriteHere={canWriteHere}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onMoveNodes={onMoveNodes}
      getSelectedNodes={getSelectedNodes}
      {...handlers}
    />
  );

  return (
    <div className={LISTING_PADDING}>
      <div ref={innerRef} className="relative" style={{ height: layout.total }}>
        {virtualizer.getVirtualItems().map((item) => {
          const row = layout.rows[item.index]!;
          return (
            <div
              key={item.key}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                width: "100%",
                height: row.height,
                transform: `translateY(${item.start - scrollMargin}px)`,
              }}
            >
              {row.kind === "label" ? (
                row.group === "dirs" ? (
                  <GroupLabel
                    label={t`Folders`}
                    count={dirs.length}
                    selected={countSelected(dirs)}
                  />
                ) : (
                  <GroupLabel
                    label={t`Files`}
                    count={files.length}
                    selected={countSelected(files)}
                  />
                )
              ) : (
                <div
                  className="grid gap-3"
                  style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}
                >
                  {row.nodes.map(card)}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/**
 * The grid before its first answer: folder pills and file cards at the real
 * sizes, in the real columns, so the data lands in place.
 */
export function NodeGridSkeleton({
  folders = 4,
  files = 10,
}: {
  folders?: number;
  files?: number;
}) {
  const label = <Skeleton className="mb-2 h-3 w-16" />;
  return (
    <LoadingStatus className={cn("@container flex flex-col gap-6", LISTING_PADDING)}>
      <section>
        {label}
        <div className={GRID}>
          {Array.from({ length: folders }, (_, i) => (
            <div key={i} className="bg-muted/60 flex h-12 items-center gap-2.5 rounded-xl px-3.5">
              <Skeleton className="size-5 rounded" />
              <Skeleton className="h-3" style={{ width: `${45 + ((i * 23) % 35)}%` }} />
            </div>
          ))}
        </div>
      </section>
      <section>
        {label}
        <div className={GRID}>
          {Array.from({ length: files }, (_, i) => (
            <div key={i} className="bg-muted/60 flex flex-col gap-2 rounded-2xl p-2.5">
              <div className="flex h-7 items-center gap-2 px-1">
                <Skeleton className="size-4 rounded" />
                <Skeleton className="h-3" style={{ width: `${40 + ((i * 29) % 40)}%` }} />
              </div>
              <Skeleton className="bg-background h-36 w-full rounded-xl" />
            </div>
          ))}
        </div>
      </section>
    </LoadingStatus>
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
  const icon = <NodeIcon node={node} className="pointer-events-none size-12 select-none" />;

  return (
    <div className="bg-background flex h-36 w-full items-center justify-center overflow-hidden rounded-xl">
      {capsOf(node).view && isMarkdownFile(node) ? (
        <MarkdownThumbnail node={node} fallback={icon} />
      ) : hasThumb ? (
        <img
          src={previewUrl(node)}
          alt=""
          loading="lazy"
          draggable={false}
          // Pictures crop to fill, pages letterbox — see thumbnailFit. The
          // classes are spelled out because Tailwind cannot see a name built
          // at runtime.
          className={cn(
            "pointer-events-none size-full",
            thumbnailFit(node) === "cover" ? "object-cover" : "object-contain",
          )}
          onError={() => setFailed(true)}
        />
      ) : (
        icon
      )}
    </div>
  );
}

interface NodeCardProps extends NodeActionHandlers {
  starred: boolean;
  shared: boolean;
  node: Node;
  canWriteHere: boolean;
  /** Clicked — feeds the bulk action bar and the details panel. */
  selected: boolean;
  onClick: (node: Node, event: MouseEvent) => void;
  onDoubleClick: (node: Node) => void;
  onMoveNodes?: (nodes: Node[], targetId: string) => Promise<void>;
  /** The selected cards, read when a drag starts. */
  getSelectedNodes: () => Node[];
}

const NodeCard = memo(function NodeCard({
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
}: NodeCardProps) {
  const { t, i18n } = useLingui();
  const f = useFormatters();
  const openPanel = useUiStore((s) => s.openPanel);
  const actions = useNodeActions(node, handlers, openPanel, starred, canWriteHere);
  const isDir = node.kind === "dir";
  const canMove = canRestructure(node);
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

  // Size and date only live in the tooltip: the Stitch card carries the name
  // alone, and the details panel and list view are where numbers belong.
  const meta = isDir
    ? [node.modified_at ? f.date(node.modified_at) : t`Folder`]
    : [
        node.size != null ? f.size(node.size) : null,
        node.modified_at ? f.date(node.modified_at) : null,
      ];
  const nodeName = node.name;
  const title = [nodeName, ...meta.filter(Boolean)].join("\n");

  const kebab = (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t`Actions for ${nodeName}`}
            className="text-muted-foreground hover:text-foreground size-6 shrink-0 rounded-full"
            onClick={(e) => e.stopPropagation()}
          />
        }
      >
        <MoreVertical />
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
  );

  const name = (
    <span className="flex min-w-0 items-center gap-1.5 truncate text-xs font-medium" title={title}>
      <span className="truncate">{nodeName}</span>
      <SubPageCount node={node} />
      <ExtOffBadge node={node} />
    </span>
  );

  return (
    <ContextMenu>
      {/* See node-list: the row owns this click, not the folder behind it. */}
      <ContextMenuTrigger onContextMenu={(e) => e.stopPropagation()}>
        <div
          data-node-id={node.id}
          draggable={canMove}
          onDragStart={handleDragStart}
          onDragEnd={handleDragEnd}
          onDragEnter={handleDragEnter}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          className={cn(
            "group bg-muted hover:bg-primary/10 dark:hover:bg-primary/20 cursor-pointer border border-transparent transition-colors",
            isDir
              ? "flex h-12 items-center justify-between rounded-xl px-3.5"
              : "h-[198px] rounded-2xl p-2.5",
            // Selection is a soft wash, not an outline: a whole folder of
            // ticked cards should still read as a grid.
            selected &&
              "ring bg-primary/10 hover:bg-primary/15 ring-primary/30 dark:bg-primary/20 dark:hover:bg-primary/25 dark:ring-primary/50",
            isDropTarget && "ring-2 ring-primary bg-primary/10 border-primary/50",
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
