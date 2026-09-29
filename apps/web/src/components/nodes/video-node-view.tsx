import type { UploadAdapter } from "@slash-editor/core";
import type { NodeViewProps } from "@tiptap/react";
import { useLingui } from "@lingui/react/macro";
import { VideoIcon } from "lucide-react";
import { UploadableNodeView } from "@/components/nodes/uploadable-node-view";
import { resolveAttachment, type AttachmentUrl } from "@/lib/collab/attachments";

/**
 * A video attachment, played in place. The attachments route serves video
 * inline with Range support, so `preload="metadata"` fetches only enough for
 * the first frame and the duration, and seeking streams from there.
 */
export function VideoNodeView(
  props: NodeViewProps & { adapter?: UploadAdapter; attachmentUrl: AttachmentUrl },
) {
  const { t } = useLingui();
  const poster = props.node.attrs.poster as string | null;

  return (
    <UploadableNodeView
      {...props}
      accept="video/*"
      icon={VideoIcon}
      emptyLabel={t`Add a video`}
      retry={(id, override) => props.editor.commands.retryVideo(id, override)}
      renderReady={(attrs) => (
        <video
          controls
          preload="metadata"
          src={resolveAttachment(attrs.src, props.attachmentUrl)}
          poster={resolveAttachment(poster, props.attachmentUrl)}
          className="bg-muted block max-h-[70vh] w-full rounded-md data-selected:ring-2 data-selected:ring-ring"
          data-selected={props.selected || undefined}
        />
      )}
    />
  );
}
