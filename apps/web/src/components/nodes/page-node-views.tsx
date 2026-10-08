import type { KeyboardEvent } from "react";
import { getPagesOptions, pageTitle, type PageMeta } from "@slash-editor/core";
import { usePage, type PageStatus } from "@slash-editor/react";
import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { FileTextIcon, FileXIcon, Trash2Icon } from "lucide-react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import { emptyPageStore } from "@/lib/collab/mdoc-kit";
import { prefetchPage } from "@/lib/collab/page-store";

/** What a page with no title reads as, wherever pp-hub draws one. */
export const UNTITLED_PAGE = msg`Untitled`;

type NodeStatus = PageStatus | "trashed";

/** The page a `subPage` or `pageLink` names, live from the editor's page store. */
function usePageNode({ editor, node }: Pick<NodeViewProps, "editor" | "node">) {
  const options = getPagesOptions(editor);
  const pageId = (node.attrs.pageId as string | null) ?? null;
  const { page, status } = usePage(options?.store ?? emptyPageStore, pageId);
  const navigate = () => {
    if (pageId) options?.onNavigate(pageId);
  };
  const warm = () => {
    if (pageId && options) prefetchPage(options.store, pageId);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    navigate();
  };
  return {
    page,
    // eslint-disable-next-line lingui/no-unlocalized-strings -- status value
    status: (page?.trashed ? "trashed" : status) as NodeStatus,
    navigate,
    warm,
    onKeyDown,
  };
}

/** The page's emoji, or a document glyph, in a box sized by the caller. */
export function PageIcon({ page, className }: { page: PageMeta | undefined; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn("flex shrink-0 items-center justify-center leading-none", className)}
    >
      {page?.icon || <FileTextIcon className="text-muted-foreground size-[0.9em]" />}
    </span>
  );
}

/** A page's title for display: its name, or "Untitled". */
export function usePageTitle(page: Pick<PageMeta, "title"> | undefined): string {
  const { i18n } = useLingui();
  return pageTitle(page, i18n._(UNTITLED_PAGE));
}

/**
 * A sub-page as a block: icon and live title — a rename anywhere shows here
 * at once — and a click or Enter opens it. A page in the trash stays a block
 * (undo brings it back) and says so; one that cannot be found, or that this
 * reader may not open, says that instead.
 */
export function SubPageNodeView({ editor, node, selected }: NodeViewProps) {
  const { page, status, navigate, warm, onKeyDown } = usePageNode({ editor, node });
  const title = usePageTitle(page);

  return (
    <NodeViewWrapper contentEditable={false} className="my-1" data-selected={selected || undefined}>
      <div
        role="link"
        tabIndex={0}
        data-status={status}
        onClick={navigate}
        onPointerEnter={warm}
        onFocus={warm}
        onKeyDown={onKeyDown}
        className="hover:bg-muted/60 focus-visible:ring-ring in-data-selected:ring-ring flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 outline-none select-none focus-visible:ring-2 in-data-selected:ring-2"
      >
        {status === "missing" ? (
          <>
            <FileXIcon className="text-muted-foreground size-5 shrink-0" />
            <span className="text-muted-foreground truncate">
              <Trans>Page not available</Trans>
            </span>
          </>
        ) : status === "loading" ? (
          <>
            <Skeleton className="size-5 shrink-0" />
            <Skeleton className="h-4 w-32" />
          </>
        ) : (
          <>
            <PageIcon page={page} className="size-5 text-lg" />
            <span
              className={cn(
                "truncate font-medium underline decoration-border underline-offset-4",
                status === "trashed" && "text-muted-foreground line-through",
              )}
            >
              {title}
            </span>
            {status === "trashed" ? (
              <span className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs">
                <Trash2Icon className="size-3" />
                <Trans>In trash</Trans>
              </span>
            ) : null}
          </>
        )}
      </div>
    </NodeViewWrapper>
  );
}

/* eslint-disable lingui/no-unlocalized-strings -- class names */
const CHIP =
  "bg-muted/60 hover:bg-muted focus-visible:ring-ring in-data-selected:ring-ring inline-flex cursor-pointer items-baseline gap-0.5 rounded-sm px-1 py-px font-medium whitespace-nowrap outline-none focus-visible:ring-2 in-data-selected:ring-2";
/* eslint-enable lingui/no-unlocalized-strings */

/** An inline link to a page: its icon and live title; a click or Enter opens it. */
export function PageLinkNodeView({ editor, node, selected }: NodeViewProps) {
  const { page, status, navigate, warm, onKeyDown } = usePageNode({ editor, node });
  const title = usePageTitle(page);

  return (
    // eslint-disable-next-line lingui/no-unlocalized-strings -- element name
    <NodeViewWrapper as="span" contentEditable={false} data-selected={selected || undefined}>
      <span
        role="link"
        tabIndex={0}
        data-status={status}
        onClick={navigate}
        onPointerEnter={warm}
        onFocus={warm}
        onKeyDown={onKeyDown}
        className={CHIP}
      >
        {status === "missing" ? (
          <span className="text-muted-foreground">
            <Trans>Page not available</Trans>
          </span>
        ) : status === "loading" ? (
          <Skeleton className="inline-block h-3 w-16 self-center" />
        ) : (
          <>
            <PageIcon page={page} className="size-[1.1em] self-center" />
            <span
              className={cn(
                "underline decoration-border underline-offset-2",
                status === "trashed" && "text-muted-foreground line-through",
              )}
            >
              {title}
            </span>
          </>
        )}
      </span>
    </NodeViewWrapper>
  );
}
