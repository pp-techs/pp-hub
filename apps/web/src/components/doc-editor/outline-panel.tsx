import type { Editor } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import { useEditorState } from "@tiptap/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { XIcon } from "lucide-react";
import { useEffect, useState, type RefObject } from "react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";

interface OutlineHeading {
  pos: number;
  level: number;
  text: string;
}

/** How far below the top of the scroller a heading still counts as the one being read. */
const ACTIVE_OFFSET = 96;

/** Every `h1`–`h3` in document order, wherever it sits (columns, toggles, …). */
function collectHeadings(doc: PMNode): OutlineHeading[] {
  const headings: OutlineHeading[] = [];
  doc.descendants((node, pos) => {
    if (node.type.name === "heading") {
      const level = node.attrs.level as number;
      if (level <= 3) headings.push({ pos, level, text: node.textContent.trim() });
      return false;
    }
    return !node.isAtom;
  });
  return headings;
}

// eslint-disable-next-line lingui/no-unlocalized-strings -- class names
const INDENT: Record<number, string> = { 1: "pl-2", 2: "pl-5", 3: "pl-8" };

/**
 * The document's headings as a table of contents beside it. The entry for
 * the section being read is marked as the reader scrolls, and a click
 * scrolls its heading to the top. It follows the live document — a heading a
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
  const headings = useEditorState({
    editor,
    selector: ({ editor: e }) => collectHeadings(e.state.doc),
  });
  const [active, setActive] = useState<number | null>(null);

  useEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const limit = scroller.getBoundingClientRect().top + ACTIVE_OFFSET;
      let current: number | null = headings[0]?.pos ?? null;
      for (const heading of headings) {
        const dom = editor.view.nodeDOM(heading.pos);
        if (!(dom instanceof HTMLElement)) continue;
        if (dom.getBoundingClientRect().top > limit) break;
        current = heading.pos;
      }
      setActive(current);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    measure();
    scroller.addEventListener("scroll", schedule, { passive: true });
    return () => {
      scroller.removeEventListener("scroll", schedule);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [editor, headings, scrollRef]);

  const jump = (pos: number) => {
    const dom = editor.view.nodeDOM(pos);
    // eslint-disable-next-line lingui/no-unlocalized-strings -- scroll options
    if (dom instanceof HTMLElement) dom.scrollIntoView({ behavior: "smooth", block: "start" });
  };

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
        {headings.length === 0 ? (
          <p className="text-muted-foreground px-2 py-1 text-xs">
            <Trans>Headings you add appear here.</Trans>
          </p>
        ) : (
          <ul className="flex flex-col">
            {headings.map((heading) => (
              <li key={heading.pos}>
                <button
                  type="button"
                  aria-current={heading.pos === active ? "location" : undefined}
                  onClick={() => jump(heading.pos)}
                  className={cn(
                    "hover:bg-muted w-full truncate rounded-md py-1 pr-2 text-left text-xs",
                    INDENT[heading.level],
                    heading.pos === active
                      ? "text-foreground font-medium"
                      : "text-muted-foreground",
                  )}
                >
                  {heading.text || t`Untitled heading`}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </nav>
  );
}
