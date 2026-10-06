import { useEffect, useRef } from "react";
import type { RefObject } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { EditorContent, useSlashEditor } from "@slash-editor/react";
import { Info } from "lucide-react";
import { TooltipProvider } from "@workspace/ui/components/tooltip";
import { cn } from "@workspace/ui/lib/utils";
import type { PageStore } from "@slash-editor/core";
import type { DocumentNodeJSON } from "@/lib/api";
import { createMdocKit, emptyPageStore } from "@/lib/collab/mdoc-kit";
import { labelToggleButton } from "@/components/doc-editor/labels";
import { OutlinePanel } from "@/components/doc-editor/outline-panel";
import {
  MDOC_NODE_VIEW_KIT_OPTIONS,
  PAGE_NODE_VIEWS,
  mdocNodeViews,
} from "@/components/nodes/mdoc-node-views";
import { DOCUMENT_COLUMN, MarkdownDocumentSkeleton } from "./skeleton";

/** The pages a rendering's sub-page blocks and links name, and where opening one goes. */
export interface ReadOnlyPages {
  store: PageStore;
  /** The document being shown. */
  pageId: string;
  onNavigate: (pageId: string) => void;
}

export interface ReadOnlyEditorProps {
  doc: DocumentNodeJSON;
  attachmentUrl: (hash: string) => string;
  lossy?: boolean;
  className?: string;
  /**
   * Show the document's headings beside it. The ref is the element the page
   * scrolls in — the outline follows that scroll, so a caller that does not
   * have one (a details-panel thumbnail, a modal page) leaves this unset and
   * gets the document alone.
   */
  outlineScrollRef?: RefObject<HTMLElement | null>;
  /**
   * Titles and icons for the document's sub-pages and page links. Without
   * it they render, but as untitled pages that open nothing.
   */
  pages?: ReadOnlyPages;
}

/**
 * The editor half of `MarkdownDocumentView`, in its own module so the
 * slash-editor bundle loads only when a document is actually shown (see
 * `index.tsx`).
 */
export default function ReadOnlyEditor({
  doc,
  attachmentUrl,
  lossy = false,
  className,
  outlineScrollRef,
  pages,
}: ReadOnlyEditorProps) {
  const { i18n } = useLingui();
  const shown = useRef(doc);
  const store = pages?.store;
  const pageId = pages?.pageId;

  const editor = useSlashEditor({
    editable: false,
    blockKit: false,
    content: doc,
    extensions: createMdocKit({
      ...MDOC_NODE_VIEW_KIT_OPTIONS,
      slash: false,
      drag: false,
      bubbleToolbar: false,
      linkEditor: false,
      placeholder: false,
      tableOfContents: { maxLevel: 3 },
      toggle: {
        renderToggleButton: ({ element, isOpen }) => labelToggleButton(i18n, element, isOpen),
      },
      extend: mdocNodeViews({ attachmentUrl }),
      // Nothing here edits, so no sub-page is ever reported detached: a
      // refetch replacing the content would otherwise read as deleting
      // every block.
      pages: {
        store: store ?? emptyPageStore,
        currentPageId: pageId ?? null,
        onNavigate: pages?.onNavigate ?? (() => {}),
        nodeViews: PAGE_NODE_VIEWS,
      },
    }),
  });

  // One listing names every sub-page block's page, before each block asks
  // for its own.
  useEffect(() => {
    if (store && pageId) Promise.resolve(store.listChildren(pageId)).catch(() => undefined);
  }, [store, pageId]);

  // A refetch (the document was edited, a version restored) hands in a new
  // tree for the same document: replace it without rebuilding the editor.
  useEffect(() => {
    if (!editor || shown.current === doc) return;
    shown.current = doc;
    editor.commands.setContent(doc, { emitUpdate: false });
  }, [editor, doc]);

  if (!editor) return <MarkdownDocumentSkeleton className={className} />;

  return (
    <TooltipProvider>
      <div className="flex gap-2 lg:gap-10">
        <div className={cn("slash-content min-w-0 flex-1", DOCUMENT_COLUMN, className)}>
          {lossy ? (
            <p className="text-muted-foreground mb-4 flex items-center gap-1.5 text-xs">
              <Info className="size-3.5 shrink-0" />
              <Trans>
                Some formatting in this file cannot be shown here. Download it to see all of it.
              </Trans>
            </p>
          ) : null}
          <EditorContent editor={editor} />
        </div>
        {/* In the right margin, as in the editor: the dashes held in view
            while the text scrolls. */}
        {outlineScrollRef ? (
          <OutlinePanel
            editor={editor}
            scrollRef={outlineScrollRef}
            className="sticky top-4 hidden self-start lg:block"
          />
        ) : null}
      </div>
    </TooltipProvider>
  );
}
