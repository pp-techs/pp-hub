import type { Editor } from "@tiptap/core";
import { useTableOfContents } from "@slash-editor/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { TableOfContents, XIcon } from "lucide-react";
import type { RefObject } from "react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";

/** How far below the top of the scroller a heading still counts as the one being read. */
const ACTIVE_OFFSET = 96;

// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const INDENT: Record<number, string> = { 1: "pl-3", 2: "pl-6", 3: "pl-9" };

/**
 * The document's headings as a table of contents, from slash-editor's
 * `tableOfContents` extension (top-level `h1`–`h3`; the kit's `maxLevel` is
 * set in `doc-editor.tsx`). The active entry follows the caret while the
 * writer types and the scroll position otherwise; a click scrolls its
 * heading to the top. It follows the live document — a heading a
 * collaborator types appears here as they type it.
 *
 * Its frame is the comments panel's (width, header, rule) because the two
 * take turns in the editor's right-hand slot. Entries hang off a guide line,
 * the active one marked on it.
 */
export function OutlinePanel({
  editor,
  scrollRef,
  onClose,
  className,
  hideWhenEmpty = false,
}: {
  editor: Editor;
  /** The element the document scrolls in. */
  scrollRef: RefObject<HTMLElement | null>;
  /** Omit for a panel that is always shown and cannot be dismissed. */
  onClose?: () => void;
  className?: string;
  /** Render nothing when the document has no headings — a reader's view. */
  hideWhenEmpty?: boolean;
}) {
  const { t } = useLingui();
  const toc = useTableOfContents(editor, {
    scrollContainer: scrollRef,
    scrollOffset: ACTIVE_OFFSET,
  });
  const { items, select } = toc;
  // Above the first heading the reader is still in its section.
  const active = toc.active ?? items[0];

  if (hideWhenEmpty && items.length === 0) return null;

  return (
    <nav
      aria-label={t`Outline`}
      className={cn(
        "border-border bg-background flex h-full w-80 shrink-0 flex-col border-l",
        className,
      )}
    >
      <div className="border-border flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <span className="text-sm font-medium">
          <Trans>Outline</Trans>
        </span>
        {onClose && (
          <Button
            variant="ghost"
            size="icon-sm"
            className="ml-auto"
            aria-label={t`Close outline`}
            onClick={onClose}
          >
            <XIcon />
          </Button>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {items.length === 0 ? (
          <div className="text-muted-foreground flex flex-col items-center gap-2 px-4 py-10 text-center text-xs">
            <TableOfContents className="size-5" />
            <Trans>Headings you add appear here.</Trans>
          </div>
        ) : (
          <ul className="border-border flex flex-col border-l">
            {items.map((item) => {
              const current = item.pos === active?.pos;
              return (
                <li key={item.id ?? item.pos}>
                  <button
                    type="button"
                    aria-current={current ? "location" : undefined}
                    onClick={() => select(item)}
                    className={cn(
                      "-ml-px w-full truncate border-l-2 py-1.5 pr-2 text-left text-sm transition-colors",
                      INDENT[item.level],
                      item.level === 1 && "font-medium",
                      current
                        ? "border-primary text-foreground"
                        : "text-muted-foreground hover:text-foreground border-transparent",
                    )}
                  >
                    {item.text.trim() || t`Untitled heading`}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </nav>
  );
}
