import type { UploadAdapter, VideoOptions } from "@slash-editor/core";
import { useResolvedSrc } from "@slash-editor/react";
import type { NodeViewProps } from "@tiptap/react";
import { useLingui } from "@lingui/react/macro";
import { VideoIcon } from "lucide-react";
import { UploadableNodeView } from "@/components/nodes/uploadable-node-view";

/**
 * A video attachment, played in place. The attachments route serves video
 * inline with Range support, so `preload="metadata"` fetches only enough for
 * the first frame and the duration, and seeking streams from there.
 */
export function VideoNodeView(props: NodeViewProps & { adapter?: UploadAdapter }) {
  const { t } = useLingui();
  const { resolveSrc } = props.extension.options as VideoOptions;
  const src = useResolvedSrc(props.node.attrs.src as string | null, props.node, resolveSrc);
  const poster = useResolvedSrc(props.node.attrs.poster as string | null, props.node, resolveSrc);

  return (
    <UploadableNodeView
      {...props}
      accept="video/*"
      icon={VideoIcon}
      emptyLabel={t`Add a video`}
      retry={(id, override) => props.editor.commands.retryVideo(id, override)}
      renderReady={() => (
        <video
          controls
          preload="metadata"
          src={src ?? undefined}
          poster={poster ?? undefined}
          className="bg-muted block max-h-[70vh] w-full rounded-md data-selected:ring-2 data-selected:ring-ring"
          data-selected={props.selected || undefined}
        />
      )}
    />
  );
}
