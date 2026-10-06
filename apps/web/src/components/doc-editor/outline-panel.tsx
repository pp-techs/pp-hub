import type { Editor } from "@tiptap/core";
import { useTableOfContents } from "@slash-editor/react";
import { useLingui } from "@lingui/react/macro";
import type { RefObject } from "react";
import { cn } from "@workspace/ui/lib/utils";

/** How far below the top of the scroller a heading still counts as the one being read. */
const ACTIVE_OFFSET = 96;

// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const DASH_WIDTH: Record<number, string> = { 1: "w-6", 2: "w-4", 3: "w-3" };
// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const INDENT: Record<number, string> = { 1: "pl-3", 2: "pl-7", 3: "pl-11" };

/**
 * The document's headings as a Notion-style table of contents, from
 * slash-editor's `tableOfContents` extension (top-level `h1`–`h3`; the kit's
 * `maxLevel` is set in `doc-editor.tsx`). At rest it is a column of short
 * dashes, one per heading, the one being read highlighted; hovering (or
 * focusing) it opens a card with the heading titles. The active entry follows
 * the caret while the writer types and the scroll position otherwise; a click
 * scrolls its heading to the top. It follows the live document — a heading a
 * collaborator types appears here as they type it. A document without
 * headings renders nothing.
 */
export function OutlinePanel({
  editor,
  scrollRef,
  className,
}: {
  editor: Editor;
  /** The element the document scrolls in. */
  scrollRef: RefObject<HTMLElement | null>;
  className?: string;
}) {
  const { t } = useLingui();
  const toc = useTableOfContents(editor, {
    scrollContainer: scrollRef,
    scrollOffset: ACTIVE_OFFSET,
  });
  const { items, select } = toc;
  // Above the first heading the reader is still in its section.
  const active = toc.active ?? items[0];

  if (items.length === 0) return null;

  return (
    <nav aria-label={t`Outline`} className={cn("group relative w-8 shrink-0 py-3", className)}>
      <ul
        aria-hidden
        className="flex max-h-[60svh] flex-col items-end gap-3 overflow-hidden transition-opacity group-focus-within:opacity-0 group-hover:opacity-0"
      >
        {items.map((item) => (
          <li
            key={item.id ?? item.pos}
            className={cn(
              "h-0.5 rounded-full",
              DASH_WIDTH[item.level],
              item.pos === active?.pos ? "bg-foreground" : "bg-muted-foreground/40",
            )}
          />
        ))}
      </ul>
      <div className="bg-popover text-popover-foreground border-border pointer-events-none absolute top-0 right-0 max-h-[70svh] w-64 overflow-y-auto rounded-2xl border p-2 opacity-0 shadow-md transition-opacity group-focus-within:pointer-events-auto group-focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100">
        <ul className="flex flex-col">
          {items.map((item) => (
            <li key={item.id ?? item.pos}>
              <button
                type="button"
                aria-current={item.pos === active?.pos ? "location" : undefined}
                onClick={() => select(item)}
                className={cn(
                  "hover:bg-muted w-full truncate rounded-lg py-1.5 pr-3 text-left text-xs",
                  INDENT[item.level],
                  item.pos === active?.pos ? "text-primary" : "text-muted-foreground",
                )}
              >
                {item.text.trim() || t`Untitled heading`}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </nav>
  );
}
