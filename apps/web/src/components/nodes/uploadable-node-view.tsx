import type { UploadAdapter, UploadStatus } from "@slash-editor/core";
import { NodeViewWrapper, useEditorState, type NodeViewProps } from "@tiptap/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Loader2, RotateCcwIcon, UploadIcon, type LucideIcon } from "lucide-react";
import { useRef, type ReactNode } from "react";
import { Button } from "@workspace/ui/components/button";

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
  /** Adapter used for a freshly picked `File` — the host's, not this component's, choice. */
  adapter: UploadAdapter;
  /** `editor.commands.retry<Type>` — bound by the per-node wrapper so this stays generic. */
  retry: (id: string, override?: { file: File; adapter: UploadAdapter }) => boolean;
  /** Renders the node once it has a `src` (ready or mid-retry after a prior success). */
  renderReady: (attrs: UploadableNodeAttrs) => ReactNode;
}

/**
 * Shared placeholder/progress/error chrome for the `image` and `file` node
 * views: core carries `status`/`error` in doc attrs, this renders them and
 * drives `retry<Type>`. Picking a file is only offered while the editor is
 * editable — a reader sees the placeholder and nothing to press.
 */
export function UploadableNodeView(props: UploadableNodeViewProps) {
  const { editor, node, accept, icon: Icon, emptyLabel, adapter, retry, renderReady } = props;
  const { t } = useLingui();
  const inputRef = useRef<HTMLInputElement>(null);
  const attrs = node.attrs as UploadableNodeAttrs;
  // `setEditable` does not re-render node views on its own.
  const editable = useEditorState({ editor, selector: ({ editor: e }) => e.isEditable });

  if (attrs.src) {
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
          if (file) retry(attrs.id, { file, adapter });
        }}
      />
    </NodeViewWrapper>
  );
}
