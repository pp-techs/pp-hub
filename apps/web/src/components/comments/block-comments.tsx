import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Editor } from "@tiptap/core";
import { useQuery } from "@tanstack/react-query";
import { useLingui } from "@lingui/react/macro";
import { MessageSquarePlus, MessageSquareText } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import type { DocCommentThread } from "@/lib/api";
import type { PpCommentStore } from "@/lib/comments";
import { commentsOptions } from "@/lib/queries";
import {
  ThreadCard,
  useThreadAnchors,
  useThreadCards,
} from "@/components/comments/comments-popover";

/**
 * A comment button at the right of every block that has an open thread; it
 * shows the block's comment count and opens its threads floating beside the
 * text. Resolved and detached threads have no block to sit by — they are in
 * the header's Comments popover.
 *
 * Mount it inside the (positioned) element the document is laid out in: it
 * covers that element and puts each button at the top of its block.
 *
 * `openThreadId` opens the block holding that thread — a thread just created
 * opens straight away — and `onOpenThreadChange` hears every open and close.
 */
export function BlockComments({
  editor,
  store,
  openThreadId,
  onOpenThreadChange,
}: {
  editor: Editor;
  store: PpCommentStore;
  openThreadId: string | null;
  onOpenThreadChange: (threadId: string | null) => void;
}) {
  const threads = useQuery(commentsOptions(store.nodeId));
  const anchors = useThreadAnchors(editor);
  const { t } = useLingui();
  const layerRef = useRef<HTMLDivElement>(null);
  const [tops, setTops] = useState<Map<number, number>>(new Map());
  const [hovered, setHovered] = useState<number | null>(null);

  // Open threads grouped by the block their text starts in, in document order.
  const blocks = useMemo(() => {
    const byBlock = new Map<number, DocCommentThread[]>();
    for (const thread of threads.data ?? []) {
      const anchor = anchors.get(thread.id);
      if (thread.resolved || !anchor) continue;
      byBlock.set(anchor.block, [...(byBlock.get(anchor.block) ?? []), thread]);
    }
    return [...byBlock].sort(([a], [b]) => a - b);
  }, [threads.data, anchors]);
  const commented = useMemo(() => new Set(blocks.map(([block]) => block)), [blocks]);

  // The text block under the pointer, while the document can be commented on:
  // it gets an "add comment" button where a commented block has its count.
  useEffect(() => {
    const area = layerRef.current?.parentElement;
    if (!area) return;
    const onMove = (event: MouseEvent) => {
      if (!editor.isEditable) return setHovered(null);
      const box = editor.view.dom.getBoundingClientRect();
      const found = editor.view.posAtCoords({
        left: Math.min(Math.max(event.clientX, box.left + 1), box.right - 1),
        top: event.clientY,
      });
      if (!found) return setHovered(null);
      const $pos = editor.state.doc.resolve(found.pos);
      setHovered($pos.parent.isTextblock && $pos.parent.content.size > 0 ? $pos.before() : null);
    };
    const clear = () => setHovered(null);
    // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM and Tiptap event names
    area.addEventListener("mousemove", onMove);
    // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM and Tiptap event names
    area.addEventListener("mouseleave", clear);
    // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM and Tiptap event names
    editor.on("update", clear);
    return () => {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM and Tiptap event names
      area.removeEventListener("mousemove", onMove);
      // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM and Tiptap event names
      area.removeEventListener("mouseleave", clear);
      // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM and Tiptap event names
      editor.off("update", clear);
    };
  }, [editor]);

  // Each button's offset from the layer. Measured from the live DOM, so it
  // follows anything that moves a block: typing above it, an image loading,
  // a toggle opening, the window narrowing.
  useLayoutEffect(() => {
    const layer = layerRef.current;
    if (!layer) return;
    const measure = () => {
      const base = layer.getBoundingClientRect().top;
      const next = new Map<number, number>();
      for (const block of hovered === null ? commented : [...commented, hovered]) {
        if (block >= editor.state.doc.content.size) continue;
        const dom = editor.view.nodeDOM(block);
        if (dom instanceof HTMLElement) next.set(block, dom.getBoundingClientRect().top - base);
      }
      setTops((prev) =>
        prev.size === next.size && [...next].every(([block, top]) => prev.get(block) === top)
          ? prev
          : next,
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(editor.view.dom);
    return () => observer.disconnect();
  }, [editor, commented, hovered]);

  return (
    <div ref={layerRef} className="pointer-events-none absolute inset-0">
      {blocks.map(([block, blockThreads]) => {
        const top = tops.get(block);
        if (top === undefined) return null;
        return (
          <BlockCommentButton
            key={block}
            editor={editor}
            store={store}
            threads={blockThreads}
            top={top}
            open={blockThreads.some((thread) => thread.id === openThreadId)}
            onOpenChange={(open) => onOpenThreadChange(open ? (blockThreads[0]?.id ?? null) : null)}
          />
        );
      })}
      {hovered !== null && !commented.has(hovered) && tops.has(hovered) ? (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t`Add a comment`}
          className="text-muted-foreground pointer-events-auto absolute right-0 md:right-18"
          style={{ top: tops.get(hovered) }}
          // Keep the editor's focus: the composer opens on the selection.
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => commentOnBlock(editor, hovered)}
        >
          <MessageSquarePlus />
        </Button>
      ) : null}
    </div>
  );
}

/** Selects a block's text and opens the composer on it. */
function commentOnBlock(editor: Editor, block: number) {
  const node = editor.state.doc.nodeAt(block);
  if (!node) return;
  editor
    .chain()
    .setTextSelection({ from: block + 1, to: block + 1 + node.content.size })
    .run();
  editor.commands.openCommentComposer();
}

function BlockCommentButton({
  editor,
  store,
  threads,
  top,
  open,
  onOpenChange,
}: {
  editor: Editor;
  store: PpCommentStore;
  threads: DocCommentThread[];
  top: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useLingui();
  const count = threads.reduce((sum, thread) => sum + thread.comments.length, 0);
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            aria-label={t`Show comments on this block`}
            className="text-muted-foreground pointer-events-auto absolute right-0 md:right-18"
            style={{ top }}
          />
        }
      >
        <MessageSquareText data-icon="inline-start" />
        {count}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        side="bottom"
        sideOffset={4}
        aria-label={t`Comments`}
        className="max-h-[min(70svh,32rem)] w-80 max-w-[calc(100vw-1rem)] gap-2 overflow-y-auto"
      >
        <BlockThreads editor={editor} store={store} threads={threads} />
      </PopoverContent>
    </Popover>
  );
}

/** The threads of one block, each open for a reply. */
function BlockThreads({
  editor,
  store,
  threads,
}: {
  editor: Editor;
  store: PpCommentStore;
  threads: DocCommentThread[];
}) {
  const { selectText, cardProps } = useThreadCards(editor, store);
  return threads.map((thread) => (
    <ThreadCard
      key={thread.id}
      {...cardProps(thread)}
      selected
      onSelect={() => selectText(thread.id, true)}
    />
  ));
}
