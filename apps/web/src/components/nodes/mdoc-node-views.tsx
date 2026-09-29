import {
  Embed,
  File as FileNode,
  findNodeById,
  Image,
  Mention,
  Mermaid,
  retryUpload,
  Video,
  type MentionOptions,
  type UploadAdapter,
} from "@slash-editor/core";
import type { AnyExtension } from "@tiptap/core";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { EmbedNodeView } from "@/components/nodes/embed-node-view";
import { FileNodeView } from "@/components/nodes/file-node-view";
import { ImageNodeView } from "@/components/nodes/image-node-view";
import { MentionNodeView } from "@/components/nodes/mention-node-view";
import { MermaidNodeView } from "@/components/nodes/mermaid-node-view";
import { VideoNodeView } from "@/components/nodes/video-node-view";
import {
  altFromFileName,
  type AttachmentResult,
  type AttachmentUrl,
} from "@/lib/collab/attachments";
import type { MdocKitOptions } from "@/lib/collab/mdoc-kit";

/**
 * The nodes `mdocNodeViews` supplies, turned off in the kit so each exists
 * once. Spread into `createMdocKit` by every surface that renders a markdown
 * document — the editor and the read-only renderer alike.
 */
export const MDOC_NODE_VIEW_KIT_OPTIONS = {
  image: false,
  file: false,
  video: false,
  embed: false,
  mermaid: false,
  mention: false,
} as const satisfies MdocKitOptions;

export interface MdocNodeViewsOptions {
  /** Where an `attachment:<hash>` reference is served for this rendering. */
  attachmentUrl: AttachmentUrl;
  /**
   * Editing only: the document uploads and link cards are made for. Without
   * it the views offer no upload or link picker, whatever the editor's
   * `isEditable` says.
   */
  editing?: { documentId: string; adapter: UploadAdapter<AttachmentResult> };
  /** Editing only: the `@` suggestions. A rendering without them still shows mentions. */
  mentionItems?: MentionOptions["items"];
}

/**
 * pp-hub's node views for the v1 block set — image, file, video, embed,
 * mermaid and mention — as the kit's own nodes extended with React views, so
 * the schema stays exactly `createMdocKit`'s. Attachments resolve through
 * `attachmentUrl`, since slash-editor has no `src` resolver hook.
 */
export function mdocNodeViews({
  attachmentUrl,
  editing,
  mentionItems,
}: MdocNodeViewsOptions): AnyExtension[] {
  const adapter = editing?.adapter;
  return [
    (adapter ? attachmentImage(adapter) : Image).extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => (
          <ImageNodeView {...props} adapter={adapter} attachmentUrl={attachmentUrl} />
        )),
    }),
    (adapter ? attachmentFile(adapter) : FileNode).extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => (
          <FileNodeView {...props} adapter={adapter} attachmentUrl={attachmentUrl} />
        )),
    }),
    Video.extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => (
          <VideoNodeView {...props} adapter={adapter} attachmentUrl={attachmentUrl} />
        )),
    }),
    Embed.extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => (
          <EmbedNodeView
            {...props}
            attachmentUrl={attachmentUrl}
            documentId={editing?.documentId}
          />
        )),
    }),
    Mermaid.extend({ addNodeView: () => ReactNodeViewRenderer(MermaidNodeView) }),
    Mention.extend({
      // eslint-disable-next-line lingui/no-unlocalized-strings -- element name
      addNodeView: () => ReactNodeViewRenderer(MentionNodeView, { as: "span" }),
    }).configure({ items: mentionItems ?? (() => []) }),
  ];
}

/**
 * The image node, naming what it shows: an upload with no alt text yet takes
 * its file name, so the markdown's `![…]` and a screen reader say something.
 * Otherwise core's `retryImage`, with the adapter pinned to ours.
 */
function attachmentImage(adapter: UploadAdapter<AttachmentResult>) {
  return Image.extend({
    addCommands() {
      return {
        ...this.parent?.(),
        retryImage:
          (id, override) =>
          ({ dispatch }) => {
            const hasEntry = override !== undefined || this.storage.pending.get(id) !== undefined;
            if (!dispatch || !hasEntry) return hasEntry;
            queueMicrotask(() => {
              retryUpload<AttachmentResult>({
                editor: this.editor,
                typeName: this.name,
                id,
                pending: this.storage.pending,
                toAttrs: (result) => {
                  const alt = findNodeById(this.editor.state.doc, id)?.node.attrs.alt as
                    | string
                    | null
                    | undefined;
                  return { src: result.url, alt: alt || altFromFileName(result.name) };
                },
                override: override ? { file: override.file, adapter } : undefined,
              });
            });
            return true;
          },
      };
    },
  });
}

/**
 * The file node, with a retry that records what the upload was. Core's own
 * `retryFile` keeps only `src`, which leaves a file attached to an empty
 * placeholder nameless and sizeless; the adapter already knows all three.
 */
function attachmentFile(adapter: UploadAdapter<AttachmentResult>) {
  return FileNode.extend({
    addCommands() {
      return {
        ...this.parent?.(),
        retryFile:
          (id, override) =>
          ({ dispatch }) => {
            const hasEntry = override !== undefined || this.storage.pending.get(id) !== undefined;
            if (!dispatch || !hasEntry) return hasEntry;
            queueMicrotask(() => {
              retryUpload<AttachmentResult>({
                editor: this.editor,
                typeName: this.name,
                id,
                pending: this.storage.pending,
                toAttrs: (result) => ({
                  src: result.url,
                  name: result.name,
                  size: result.size,
                  mime: result.mime,
                }),
                override: override ? { file: override.file, adapter } : undefined,
              });
            });
            return true;
          },
      };
    },
  });
}
