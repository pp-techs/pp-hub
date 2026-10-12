import type { FileOptions, UploadAdapter } from "@slash-editor/core";
import { useResolvedSrc } from "@slash-editor/react";
import type { NodeViewProps } from "@tiptap/react";
import { useLingui } from "@lingui/react/macro";
import { PaperclipIcon } from "lucide-react";
import type { RelativeEmbed } from "@/lib/api";
import { UploadableNodeView } from "@/components/nodes/uploadable-node-view";
import { useFormatters } from "@/lib/format";

export function FileNodeView(
  props: NodeViewProps & {
    adapter?: UploadAdapter;
    embed?: (path: string) => Promise<RelativeEmbed>;
  },
) {
  const { t } = useLingui();
  const f = useFormatters();
  const name = (props.node.attrs.name as string | null) ?? t`Untitled file`;
  const size = props.node.attrs.size as number | null;
  const href = useResolvedSrc(
    props.node.attrs.src as string | null,
    props.node,
    (props.extension.options as FileOptions).resolveSrc,
  );

  return (
    <UploadableNodeView
      {...props}
      accept="*/*"
      icon={PaperclipIcon}
      emptyLabel={t`Add a file`}
      retry={(id, override) => props.editor.commands.retryFile(id, override)}
      // The card shows what it holds; the embedded copy says it now.
      embedAttrs={({ src, size: bytes, mime }) => ({ src, size: bytes, mime })}
      renderReady={() => (
        <a
          href={href ?? undefined}
          download={name}
          target="_blank"
          rel="noopener noreferrer"
          className="border-border bg-card hover:bg-muted/60 flex items-center gap-2 rounded-md border p-3 no-underline"
        >
          <PaperclipIcon className="text-muted-foreground size-4 shrink-0" />
          <span className="flex-1 truncate text-sm font-medium">{name}</span>
          {size != null ? (
            <span className="text-muted-foreground text-xs">{f.size(size)}</span>
          ) : null}
        </a>
      )}
    />
  );
}
