import { useState, type DragEvent, type KeyboardEvent } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import type { PageMeta } from "@slash-editor/core";
import { usePage, usePageTree } from "@slash-editor/react";
import { ChevronRightIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import type { PpPageMeta, PpPageStore } from "@/lib/collab/page-store";
import { PageIcon, usePageTitle } from "@/components/nodes/page-node-views";

/** Where a drop lands on a row: above it, below it, or under it as a sub-page. */
type DropZone = "before" | "after" | "into";

/** An index past any sibling list: the server clamps it, so the page goes last. */
const LAST = 2 ** 31 - 1;

/* eslint-disable lingui/no-unlocalized-strings -- drop zone names and DataTransfer tokens */
/** The part of a row the pointer is over; the root takes drops only on itself. */
function zoneAt(event: DragEvent<HTMLElement>, depth: number): DropZone {
  if (depth === 0) return "into";
  const box = event.currentTarget.getBoundingClientRect();
  const at = (event.clientY - box.top) / box.height;
  return at < 0.25 ? "before" : at > 0.75 ? "after" : "into";
}

/** Starts dragging a page; Firefox starts no drag without data. */
function pickUp(event: DragEvent<HTMLElement>, pageId: string) {
  event.dataTransfer.effectAllowed = "move";
  event.dataTransfer.setData("text/plain", pageId);
}

/** Marks the row under the pointer as a place the dragged page may be dropped. */
function allowDrop(event: DragEvent<HTMLElement>) {
  event.preventDefault();
  event.dataTransfer.dropEffect = "move";
}
/* eslint-enable lingui/no-unlocalized-strings */

/** Left padding per depth — static so Tailwind sees every class. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const INDENT = ["pl-1", "pl-5", "pl-9", "pl-13", "pl-17", "pl-21", "pl-25", "pl-29"];

/**
 * The page tree beside the document, rooted at the open page's top-most
 * ancestor the reader can view (or the page itself): the root, then its
 * sub-pages, each expanding lazily. The open page's ancestors start
 * expanded, so it is in view; its row is highlighted. A writer's `+` adds a
 * sub-page to the open page — at the end of its document, as `/page` would.
 *
 * A writer can also drag a row onto another row — above or below it to
 * reorder, over its middle to make it a sub-page — or press Alt+↑/↓ to move
 * it among its siblings, and trash a row with its button (undoable).
 */
export function PageTreePanel({
  store,
  rootId,
  activeId,
  expanded,
  onNavigate,
  onCreate,
  onClose,
  editable,
}: {
  store: PpPageStore;
  rootId: string;
  activeId: string;
  /** Pages expanded on mount: the open page's ancestors. */
  expanded: readonly string[];
  onNavigate: (pageId: string) => void;
  onCreate?: () => void;
  /** Whether rows can be dragged, moved with the keyboard and trashed. */
  editable: boolean;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const root = usePage(store, rootId);
  const tree = usePageTree(store, rootId, { defaultExpanded: expanded });
  const rows = [
    ...(root.page ? [{ page: root.page, depth: 0, expanded: true }] : []),
    ...tree.rows.map((row) => ({ ...row, depth: row.depth + 1 })),
  ];
  const [drag, setDrag] = useState<{ id: string; over: { id: string; zone: DropZone } | null }>();

  const fail = (title: string) => (error: unknown) =>
    toast.error(title, { description: error instanceof Error ? error.message : undefined });

  /** Whether the row at `index` is the page `ancestorId` or lies under it, as the rows are laid out. */
  const within = (index: number, ancestorId: string) => {
    const from = rows.findIndex((row) => row.page.id === ancestorId);
    if (from < 0 || index < from) return false;
    return rows.slice(from + 1, index + 1).every((row) => row.depth > rows[from]!.depth);
  };

  /** The shown sub-pages of `parentId` but `exceptId`: the list `move`'s index counts in. */
  const siblingsOf = (parentId: string | null, exceptId: string) =>
    rows.filter(
      (row) => row.depth > 0 && row.page.parentId === parentId && row.page.id !== exceptId,
    );

  const drop = (movingId: string, index: number, zone: DropZone) => {
    const row = rows[index]!;
    const parentId = zone === "into" ? row.page.id : row.page.parentId;
    if (!parentId) return;
    const at =
      zone === "into"
        ? LAST
        : siblingsOf(parentId, movingId).findIndex((sibling) => sibling.page.id === row.page.id) +
          (zone === "after" ? 1 : 0);
    if (zone === "into") tree.expand(parentId);
    store.move(movingId, { parentId, index: at }).catch(fail(t`Could not move the page`));
  };

  /** Moves the page at `index` one place up (-1) or down (1) among its siblings. */
  const moveBy = (index: number, step: -1 | 1) => {
    const { page } = rows[index]!;
    const siblings = rows.filter((row) => row.depth > 0 && row.page.parentId === page.parentId);
    const to = siblings.findIndex((row) => row.page.id === page.id) + step;
    if (!page.parentId || to < 0 || to >= siblings.length) return;
    store
      .move(page.id, { parentId: page.parentId, index: to })
      .catch(fail(t`Could not move the page`));
  };

  const trash = (index: number) => {
    const { page } = rows[index]!;
    const open = within(
      rows.findIndex((row) => row.page.id === activeId),
      page.id,
    );
    store
      .setTrashed([page.id], true)
      .then(() => {
        // The page on screen went with it: go to where it was.
        if (open) onNavigate(page.parentId ?? rootId);
        toast(t`Moved to the trash`, {
          action: {
            label: t`Undo`,
            onClick: () =>
              store.setTrashed([page.id], false).catch(fail(t`Could not restore the page`)),
          },
        });
      })
      .catch(fail(t`Could not move the page to the trash`));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLElement>, index: number) => {
    if (event.target !== event.currentTarget) return;
    const row = rows[index]!;
    const items = event.currentTarget.parentElement?.querySelectorAll<HTMLElement>(
      // eslint-disable-next-line lingui/no-unlocalized-strings -- CSS selector
      '[role="treeitem"]',
    );
    const focusRow = (at: number) => items?.[at]?.focus();
    if (editable && index > 0 && event.altKey && event.key === "ArrowUp") {
      moveBy(index, -1);
      event.preventDefault();
      return;
    }
    if (editable && index > 0 && event.altKey && event.key === "ArrowDown") {
      moveBy(index, 1);
      event.preventDefault();
      return;
    }
    switch (event.key) {
      case "Enter":
        onNavigate(row.page.id);
        break;
      case "ArrowDown":
        focusRow(index + 1);
        break;
      case "ArrowUp":
        focusRow(index - 1);
        break;
      case "ArrowRight":
        if (index === 0) focusRow(1);
        else if (!row.expanded) tree.expand(row.page.id);
        else if ((rows[index + 1]?.depth ?? 0) > row.depth) focusRow(index + 1);
        break;
      case "ArrowLeft":
        if (index > 0 && row.expanded) tree.collapse(row.page.id);
        else focusRow(rows.findLastIndex((r, at) => at < index && r.depth < row.depth));
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  return (
    <nav
      aria-label={t`Pages`}
      className="border-border bg-background flex w-48 shrink-0 flex-col border-r"
    >
      <div className="flex h-10 shrink-0 items-center justify-between gap-1 pr-1 pl-3">
        <span className="text-muted-foreground text-xs font-medium">
          <Trans>Pages</Trans>
        </span>
        <div className="flex items-center">
          {onCreate ? (
            <Button variant="ghost" size="icon-sm" aria-label={t`New sub-page`} onClick={onCreate}>
              <PlusIcon />
            </Button>
          ) : null}
          <Button variant="ghost" size="icon-sm" aria-label={t`Close pages`} onClick={onClose}>
            <XIcon />
          </Button>
        </div>
      </div>
      <div
        role="tree"
        className="flex min-h-0 flex-1 flex-col overflow-y-auto px-1 pb-4"
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
            setDrag((current) => current && { ...current, over: null });
          }
        }}
      >
        {root.status === "loading" ? (
          <div className="flex flex-col gap-2 px-2 py-1">
            <Skeleton className="h-4 w-36" />
            <Skeleton className="ml-4 h-4 w-28" />
          </div>
        ) : null}
        {rows.map((row, index) => (
          <TreeRow
            key={row.page.id}
            page={row.page}
            depth={row.depth}
            expanded={row.expanded}
            active={row.page.id === activeId}
            canToggle={index > 0}
            onToggle={() => tree.toggle(row.page.id)}
            onNavigate={() => onNavigate(row.page.id)}
            onWarm={() => store.prefetch(row.page.id)}
            onKeyDown={(event) => onKeyDown(event, index)}
            dropZone={drag?.over?.id === row.page.id ? drag.over.zone : null}
            edit={
              editable && index > 0
                ? {
                    onTrash: () => trash(index),
                    onDragStart: (event) => {
                      pickUp(event, row.page.id);
                      setDrag({ id: row.page.id, over: null });
                    },
                    onDragEnd: () => setDrag(undefined),
                  }
                : undefined
            }
            onDragOver={
              drag && !within(index, drag.id)
                ? (event) => {
                    allowDrop(event);
                    const zone = zoneAt(event, row.depth);
                    if (drag.over?.id !== row.page.id || drag.over.zone !== zone) {
                      setDrag({ id: drag.id, over: { id: row.page.id, zone } });
                    }
                  }
                : undefined
            }
            onDrop={
              drag
                ? (event) => {
                    event.preventDefault();
                    drop(drag.id, index, zoneAt(event, row.depth));
                    setDrag(undefined);
                  }
                : undefined
            }
          />
        ))}
      </div>
    </nav>
  );
}

function TreeRow({
  page,
  depth,
  expanded,
  active,
  canToggle,
  onToggle,
  onNavigate,
  onWarm,
  onKeyDown,
  dropZone,
  edit,
  onDragOver,
  onDrop,
}: {
  page: PageMeta;
  depth: number;
  expanded: boolean;
  active: boolean;
  canToggle: boolean;
  onToggle: () => void;
  onNavigate: () => void;
  onWarm: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
  /** Where the page being dragged would land on this row. */
  dropZone: DropZone | null;
  /** Present for a row a writer can drag and trash. */
  edit?: {
    onTrash: () => void;
    onDragStart: (event: DragEvent<HTMLElement>) => void;
    onDragEnd: () => void;
  };
  /** Present while a drag is on and this row can take the dragged page. */
  onDragOver?: (event: DragEvent<HTMLElement>) => void;
  onDrop?: (event: DragEvent<HTMLElement>) => void;
}) {
  const { t } = useLingui();
  const title = usePageTitle(page);
  // The tree knows a page has sub-pages before it lists them; an expanded
  // row keeps its chevron so it can be folded again.
  const hasChildren = (page as PpPageMeta).hasChildren || expanded;

  return (
    <div
      role="treeitem"
      tabIndex={0}
      aria-selected={active}
      aria-expanded={hasChildren ? expanded : undefined}
      aria-current={active ? "page" : undefined}
      onClick={onNavigate}
      onPointerEnter={onWarm}
      onFocus={onWarm}
      onKeyDown={onKeyDown}
      draggable={edit ? true : undefined}
      onDragStart={edit?.onDragStart}
      onDragEnd={edit?.onDragEnd}
      onDragOver={onDragOver}
      onDrop={onDrop}
      className={cn(
        "group hover:bg-muted focus-visible:ring-ring flex cursor-pointer items-center gap-1 rounded-md py-1 pr-2 text-xs outline-none focus-visible:ring-2",
        INDENT[Math.min(depth, INDENT.length - 1)],
        active ? "bg-muted text-foreground font-medium" : "text-muted-foreground",
        dropZone === "into" && "bg-accent ring-primary ring-1",
        dropZone === "before" && "shadow-[inset_0_2px_0_0_var(--primary)]",
        dropZone === "after" && "shadow-[inset_0_-2px_0_0_var(--primary)]",
      )}
    >
      {canToggle && hasChildren ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          tabIndex={-1}
          aria-label={expanded ? t`Collapse` : t`Expand`}
          onClick={(event) => {
            event.stopPropagation();
            onToggle();
          }}
        >
          <ChevronRightIcon className={cn("transition-transform", expanded && "rotate-90")} />
        </Button>
      ) : (
        <span aria-hidden className="size-5 shrink-0" />
      )}
      <PageIcon page={page} className="size-4 text-sm" />
      <span className="truncate">{title}</span>
      {edit ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          tabIndex={-1}
          aria-label={t`Move to trash`}
          className="ml-auto opacity-0 group-hover:opacity-100 group-focus:opacity-100"
          onClick={(event) => {
            event.stopPropagation();
            edit.onTrash();
          }}
        >
          <Trash2Icon />
        </Button>
      ) : null}
    </div>
  );
}
