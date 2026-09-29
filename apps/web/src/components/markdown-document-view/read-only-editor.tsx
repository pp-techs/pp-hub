import { useEffect, useRef } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { EditorContent, useSlashEditor } from "@slash-editor/react";
import { Info } from "lucide-react";
import { TooltipProvider } from "@workspace/ui/components/tooltip";
import { cn } from "@workspace/ui/lib/utils";
import type { DocumentNodeJSON } from "@/lib/api";
import { createMdocKit } from "@/lib/collab/mdoc-kit";
import { labelToggleButton } from "@/components/doc-editor/labels";
import { MDOC_NODE_VIEW_KIT_OPTIONS, mdocNodeViews } from "@/components/nodes/mdoc-node-views";
import { DOCUMENT_COLUMN, MarkdownDocumentSkeleton } from "./skeleton";

export interface ReadOnlyEditorProps {
  doc: DocumentNodeJSON;
  attachmentUrl: (hash: string) => string;
  lossy?: boolean;
  className?: string;
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
}: ReadOnlyEditorProps) {
  const { i18n } = useLingui();
  const shown = useRef(doc);

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
      toggle: {
        renderToggleButton: ({ element, isOpen }) => labelToggleButton(i18n, element, isOpen),
      },
      extend: mdocNodeViews({ attachmentUrl }),
    }),
  });

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
      <div className={cn("slash-content", DOCUMENT_COLUMN, className)}>
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
    </TooltipProvider>
  );
}
