import type { KeyboardEvent } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import type { PageMeta } from "@slash-editor/core";
import { usePage, usePageTree } from "@slash-editor/react";
import { ChevronRightIcon, PlusIcon, XIcon } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import type { PpPageMeta, PpPageStore } from "@/lib/collab/page-store";
import { PageIcon, usePageTitle } from "@/components/nodes/page-node-views";

/** Left padding per depth — static so Tailwind sees every class. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const INDENT = ["pl-1", "pl-5", "pl-9", "pl-13", "pl-17", "pl-21", "pl-25", "pl-29"];

/**
 * The page tree beside the document, rooted at the open page's top-most
 * ancestor the reader can view (or the page itself): the root, then its
 * sub-pages, each expanding lazily. The open page's ancestors start
 * expanded, so it is in view; its row is highlighted. A writer's `+` adds a
 * sub-page to the open page — at the end of its document, as `/page` would.
 */
export function PageTreePanel({
  store,
  rootId,
  activeId,
  expanded,
  onNavigate,
  onCreate,
  onClose,
}: {
  store: PpPageStore;
  rootId: string;
  activeId: string;
  /** Pages expanded on mount: the open page's ancestors. */
  expanded: readonly string[];
  onNavigate: (pageId: string) => void;
  onCreate?: () => void;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const root = usePage(store, rootId);
  const tree = usePageTree(store, rootId, { defaultExpanded: expanded });
  const rows = [
    ...(root.page ? [{ page: root.page, depth: 0, expanded: true }] : []),
    ...tree.rows.map((row) => ({ ...row, depth: row.depth + 1 })),
  ];

  const onKeyDown = (event: KeyboardEvent<HTMLElement>, index: number) => {
    if (event.target !== event.currentTarget) return;
    const row = rows[index]!;
    const items = event.currentTarget.parentElement?.querySelectorAll<HTMLElement>(
      // eslint-disable-next-line lingui/no-unlocalized-strings -- CSS selector
      '[role="treeitem"]',
    );
    const focusRow = (at: number) => items?.[at]?.focus();
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
      className="border-border bg-background flex w-60 shrink-0 flex-col border-r"
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
      <div role="tree" className="flex min-h-0 flex-1 flex-col overflow-y-auto px-1 pb-4">
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
            onKeyDown={(event) => onKeyDown(event, index)}
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
  onKeyDown,
}: {
  page: PageMeta;
  depth: number;
  expanded: boolean;
  active: boolean;
  canToggle: boolean;
  onToggle: () => void;
  onNavigate: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
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
      onKeyDown={onKeyDown}
      className={cn(
        "hover:bg-muted focus-visible:ring-ring flex cursor-pointer items-center gap-1 rounded-md py-1 pr-2 text-xs outline-none focus-visible:ring-2",
        INDENT[Math.min(depth, INDENT.length - 1)],
        active ? "bg-muted text-foreground font-medium" : "text-muted-foreground",
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
    </div>
  );
}
