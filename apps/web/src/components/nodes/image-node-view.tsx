import type { ImageOptions, UploadAdapter } from "@slash-editor/core";
import { useResolvedSrc } from "@slash-editor/react";
import { useEditorState, type NodeViewProps } from "@tiptap/react";
import { useLingui } from "@lingui/react/macro";
import { ImageIcon } from "lucide-react";
import { useRef, useState, type PointerEvent } from "react";
import type { RelativeEmbed } from "@/lib/api";
import { isRelativeRef } from "@/lib/collab/document-files";
import { UnresolvedReference, UploadableNodeView } from "@/components/nodes/uploadable-node-view";

/** Narrowest an image can be dragged to, in CSS pixels. */
const MIN_WIDTH = 80;

export function ImageNodeView(
  props: NodeViewProps & {
    adapter?: UploadAdapter;
    embed?: (path: string) => Promise<RelativeEmbed>;
  },
) {
  const { t } = useLingui();
  const src = useResolvedSrc(
    props.node.attrs.src as string | null,
    props.node,
    (props.extension.options as ImageOptions).resolveSrc,
  );
  // A relative path that fails to load names nothing this reader can see.
  const [failed, setFailed] = useState<string | null>(null);
  const stored = props.node.attrs.src as string | null;
  return (
    <UploadableNodeView
      {...props}
      accept="image/*"
      icon={ImageIcon}
      emptyLabel={t`Add an image`}
      retry={(id, override) => props.editor.commands.retryImage(id, override)}
      renderReady={() =>
        isRelativeRef(stored) && failed === src ? (
          <UnresolvedReference icon={ImageIcon} path={stored} />
        ) : (
          <ResizableImage
            {...props}
            src={src ?? undefined}
            alt={(props.node.attrs.alt as string | null) ?? ""}
            onError={() => setFailed(src)}
          />
        )
      }
    />
  );
}

/**
 * The image at its stored `width` (the markdown keeps it in a
 * `<!-- slash:image {"width":…} -->` marker), or the column's width when it
 * has none. A writer drags the right edge to resize; the new width is written
 * once, on release, so a drag is one change for every collaborator rather
 * than a stream of them.
 */
function ResizableImage({
  editor,
  node,
  selected,
  updateAttributes,
  src,
  alt,
  onError,
}: NodeViewProps & { src: string | undefined; alt: string; onError: () => void }) {
  const { t } = useLingui();
  const imageRef = useRef<HTMLImageElement>(null);
  const [dragWidth, setDragWidth] = useState<number | null>(null);
  const editable = useEditorState({ editor, selector: ({ editor: e }) => e.isEditable });
  const stored = typeof node.attrs.width === "number" ? node.attrs.width : null;
  const width = dragWidth ?? stored;

  const startResize = (event: PointerEvent<HTMLSpanElement>) => {
    const image = imageRef.current;
    const column = image?.parentElement?.parentElement;
    if (!image || !column) return;
    event.preventDefault();
    const handle = event.currentTarget;
    handle.setPointerCapture(event.pointerId);
    const startX = event.clientX;
    const startWidth = image.getBoundingClientRect().width;
    const maxWidth = column.getBoundingClientRect().width;
    let next = startWidth;

    const move = (e: globalThis.PointerEvent) => {
      next = Math.round(Math.min(maxWidth, Math.max(MIN_WIDTH, startWidth + e.clientX - startX)));
      setDragWidth(next);
    };
    const end = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
      setDragWidth(null);
      // Full column width is the default; storing it would pin the image to
      // this screen's column instead of following the reader's.
      updateAttributes({ width: next >= maxWidth ? null : next });
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  };

  return (
    <div className="relative w-fit max-w-full" data-selected={selected || undefined}>
      <img
        ref={imageRef}
        src={src}
        alt={alt}
        draggable={false}
        onError={onError}
        style={width ? { width } : undefined}
        className="block max-w-full rounded-md in-data-selected:ring-2 in-data-selected:ring-ring"
      />
      {editable ? (
        <span
          role="separator"
          aria-orientation="vertical"
          aria-label={t`Resize image`}
          onPointerDown={startResize}
          className="bg-background/80 ring-border absolute top-1/2 right-1.5 hidden h-10 w-1.5 -translate-y-1/2 cursor-ew-resize rounded-full ring-1 group-hover:block in-data-selected:block"
        />
      ) : null}
    </div>
  );
}
