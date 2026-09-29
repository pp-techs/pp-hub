import type { Editor } from "@tiptap/core";
import { useTableOfContents } from "@slash-editor/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { XIcon } from "lucide-react";
import type { RefObject } from "react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";

/** How far below the top of the scroller a heading still counts as the one being read. */
const ACTIVE_OFFSET = 96;

// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const INDENT: Record<number, string> = { 1: "pl-2", 2: "pl-5", 3: "pl-8" };

/**
 * The document's headings as a table of contents beside it, from
 * slash-editor's `tableOfContents` extension (top-level `h1`–`h3`; the kit's
 * `maxLevel` is set in `doc-editor.tsx`). The active entry follows the caret
 * while the writer types and the scroll position otherwise; a click scrolls
 * its heading to the top. It follows the live document — a heading a
 * collaborator types appears here as they type it.
 */
export function OutlinePanel({
  editor,
  scrollRef,
  onClose,
}: {
  editor: Editor;
  /** The element the document scrolls in. */
  scrollRef: RefObject<HTMLElement | null>;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const toc = useTableOfContents(editor, {
    scrollContainer: scrollRef,
    scrollOffset: ACTIVE_OFFSET,
  });
  const { items, select } = toc;
  // Above the first heading the reader is still in its section.
  const active = toc.active ?? items[0];

  return (
    <nav
      aria-label={t`Outline`}
      className="border-border bg-background flex w-60 shrink-0 flex-col border-r"
    >
      <div className="flex h-10 shrink-0 items-center justify-between gap-2 pr-1 pl-3">
        <span className="text-muted-foreground text-xs font-medium">
          <Trans>Outline</Trans>
        </span>
        <Button variant="ghost" size="icon-sm" aria-label={t`Close outline`} onClick={onClose}>
          <XIcon />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-1 pb-4">
        {items.length === 0 ? (
          <p className="text-muted-foreground px-2 py-1 text-xs">
            <Trans>Headings you add appear here.</Trans>
          </p>
        ) : (
          <ul className="flex flex-col">
            {items.map((item) => (
              <li key={item.id ?? item.pos}>
                <button
                  type="button"
                  aria-current={item.pos === active?.pos ? "location" : undefined}
                  onClick={() => select(item)}
                  className={cn(
                    "hover:bg-muted w-full truncate rounded-md py-1 pr-2 text-left text-xs",
                    INDENT[item.level],
                    item.pos === active?.pos
                      ? "text-foreground font-medium"
                      : "text-muted-foreground",
                  )}
                >
                  {item.text.trim() || t`Untitled heading`}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </nav>
  );
}
