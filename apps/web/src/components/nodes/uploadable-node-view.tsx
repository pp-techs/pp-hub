import type { UploadAdapter, UploadStatus } from "@slash-editor/core";
import { NodeViewWrapper, useEditorState, type NodeViewProps } from "@tiptap/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Loader2, PinIcon, RotateCcwIcon, UploadIcon, type LucideIcon } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import type { RelativeEmbed } from "@/lib/api";
import { isRelativeRef } from "@/lib/collab/document-files";

export interface UploadableNodeAttrs {
  id: string;
  src: string | null;
  status: UploadStatus;
  error: string | null;
}

interface UploadableNodeViewProps extends NodeViewProps {
  /** Native file input `accept` filter. */
  accept: string;
  icon: LucideIcon;
  emptyLabel: string;
  /**
   * Adapter used for a freshly picked `File` — the host's, not this
   * component's, choice. A read-only rendering has none and offers no picker.
   */
  adapter?: UploadAdapter;
  /** `editor.commands.retry<Type>` — bound by the per-node wrapper so this stays generic. */
  retry: (id: string, override?: { file: File; adapter: UploadAdapter }) => boolean;
  /** Renders the node once it has a `src` (ready or mid-retry after a prior success). */
  renderReady: (attrs: UploadableNodeAttrs) => ReactNode;
  /**
   * Editing only: copies the file a relative `src` names into the document's
   * attachments. Offered on a block whose `src` is a relative path.
   */
  embed?: (path: string) => Promise<RelativeEmbed>;
  /** The attributes an embedded file leaves on the block; its `src` alone by default. */
  embedAttrs?: (embedded: RelativeEmbed) => Record<string, unknown>;
}

/**
 * Shared placeholder/progress/error chrome for the uploadable node views
 * (image, file, video): core carries `status`/`error` in doc attrs, this
 * renders them and drives `retry<Type>`. Picking a file is only offered while
 * the editor is editable and has an adapter — a reader sees the placeholder
 * and nothing to press. A block pointing at a file beside the document by a
 * relative path offers a writer to embed it: the document then keeps its own
 * copy, whatever happens to the file.
 */
export function UploadableNodeView(props: UploadableNodeViewProps) {
  const { editor, node, accept, icon: Icon, emptyLabel, adapter, retry, renderReady } = props;
  const { embed, embedAttrs, updateAttributes } = props;
  const { t } = useLingui();
  const inputRef = useRef<HTMLInputElement>(null);
  const [embedding, setEmbedding] = useState(false);
  const attrs = node.attrs as UploadableNodeAttrs;
  // `setEditable` does not re-render node views on its own.
  const editable =
    useEditorState({ editor, selector: ({ editor: e }) => e.isEditable }) && !!adapter;

  const embedFile = (path: string) => {
    if (!embed) return;
    setEmbedding(true);
    embed(path)
      .then(
        (embedded) => updateAttributes(embedAttrs?.(embedded) ?? { src: embedded.src }),
        (error: unknown) =>
          toast.error(t`Could not embed the file`, {
            description: error instanceof Error ? error.message : undefined,
          }),
      )
      .finally(() => setEmbedding(false));
  };

  if (attrs.src) {
    const src = attrs.src;
    return (
      <NodeViewWrapper data-status={attrs.status} className="group relative my-2">
        {renderReady(attrs)}
        {attrs.status === "error" && editable ? (
          <Button
            type="button"
            size="icon-sm"
            variant="secondary"
            className="absolute top-2 right-2"
            aria-label={t`Retry upload`}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => retry(attrs.id)}
          >
            <RotateCcwIcon />
          </Button>
        ) : editable && embed && isRelativeRef(src) ? (
          <Button
            type="button"
            size="icon-sm"
            variant="secondary"
            className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
            aria-label={t`Embed in document`}
            title={t`Embed in document`}
            disabled={embedding}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => embedFile(src)}
          >
            {embedding ? <Loader2 className="animate-spin" /> : <PinIcon />}
          </Button>
        ) : null}
      </NodeViewWrapper>
    );
  }

  const message =
    attrs.status === "uploading"
      ? t`Uploading…`
      : attrs.status === "error"
        ? (attrs.error ?? t`Upload failed`)
        : emptyLabel;

  return (
    <NodeViewWrapper
      data-status={attrs.status}
      data-error={attrs.error ?? undefined}
      contentEditable={false}
      className="border-border bg-muted/40 text-muted-foreground my-2 flex items-center gap-2 rounded-md border border-dashed p-4"
    >
      {attrs.status === "uploading" ? (
        <Loader2 className="size-4 shrink-0 animate-spin" />
      ) : (
        <Icon className="size-4 shrink-0" />
      )}
      <span className="flex-1 truncate text-sm">{message}</span>
      {!editable ? null : attrs.status === "error" ? (
        <Button
          type="button"
          size="sm"
          variant="secondary"
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => retry(attrs.id)}
        >
          <RotateCcwIcon data-icon="inline-start" />
          <Trans>Retry</Trans>
        </Button>
      ) : attrs.status !== "uploading" ? (
        <Button
          type="button"
          size="sm"
          variant="secondary"
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => inputRef.current?.click()}
        >
          <UploadIcon data-icon="inline-start" />
          <Trans>Upload</Trans>
        </Button>
      ) : null}
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        className="hidden"
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file && adapter) retry(attrs.id, { file, adapter });
        }}
      />
    </NodeViewWrapper>
  );
}

/**
 * What a block shows when the file its relative path names cannot be shown:
 * gone, renamed, moved, or not this reader's to see. The path is what the
 * markdown says, so a writer can tell what to fix.
 */
export function UnresolvedReference({ icon: Icon, path }: { icon: LucideIcon; path: string }) {
  const { t } = useLingui();
  return (
    <div
      contentEditable={false}
      className="border-border bg-muted/40 text-muted-foreground flex items-center gap-2 rounded-md border border-dashed p-4"
    >
      <Icon className="size-4 shrink-0" />
      <span className="flex-1 truncate text-sm">{t`Cannot show ${path}`}</span>
    </div>
  );
}
