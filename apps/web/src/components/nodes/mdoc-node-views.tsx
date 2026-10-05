import {
  Embed,
  File as FileNode,
  Image,
  Mention,
  Mermaid,
  Video,
  withPageMentions,
  type MentionOptions,
  type PagesOptions,
  type UploadAdapter,
  type UploadResult,
} from "@slash-editor/core";
import type { AnyExtension } from "@tiptap/core";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { EmbedNodeView } from "@/components/nodes/embed-node-view";
import { FileNodeView } from "@/components/nodes/file-node-view";
import { ImageNodeView } from "@/components/nodes/image-node-view";
import { MentionNodeView } from "@/components/nodes/mention-node-view";
import { PageLinkNodeView, SubPageNodeView } from "@/components/nodes/page-node-views";
import { MermaidNodeView } from "@/components/nodes/mermaid-node-view";
import { VideoNodeView } from "@/components/nodes/video-node-view";
import {
  altFromFileName,
  attachmentResolver,
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
  /**
   * Editing only: the kit's `pages`, whose store's pages the `@` menu offers
   * after `mentionItems`. The kit merges them only into its own `mention`,
   * which `MDOC_NODE_VIEW_KIT_OPTIONS` turns off for this one.
   */
  pages?: PagesOptions;
}

/**
 * The page nodes' views, for `createMdocKit({ pages: { nodeViews } })`: the
 * kit registers `subPage` and `pageLink` itself, so they cannot be extended
 * here like the others.
 */
export const PAGE_NODE_VIEWS: NonNullable<PagesOptions["nodeViews"]> = {
  subPage: ReactNodeViewRenderer(SubPageNodeView),
  // eslint-disable-next-line lingui/no-unlocalized-strings -- element name
  pageLink: ReactNodeViewRenderer(PageLinkNodeView, { as: "span" }),
};

/**
 * pp-hub's node views for the v1 block set — image, file, video, embed,
 * mermaid and mention — as the kit's own nodes extended with React views, so
 * the schema stays exactly `createMdocKit`'s. Each media node's `resolveSrc`
 * maps `attachment:<hash>` through `attachmentUrl`; the views read it from
 * their extension's options. The page nodes' views travel separately, as
 * `PAGE_NODE_VIEWS`.
 *
 * An upload records what it was: an image takes its file name as alt text
 * (the markdown's `![…]`, and what a screen reader says), a file its name,
 * size and type, so the block shows them before anyone opens it.
 */
export function mdocNodeViews({
  attachmentUrl,
  editing,
  mentionItems,
  pages,
}: MdocNodeViewsOptions): AnyExtension[] {
  const adapter = editing?.adapter;
  const resolveSrc = attachmentResolver(attachmentUrl);
  const items = mentionItems ?? (() => []);
  return [
    Image.extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => <ImageNodeView {...props} adapter={adapter} />),
    }).configure({
      resolveSrc,
      toAttrs: (result: UploadResult) => ({
        src: result.url,
        alt: altFromFileName((result as AttachmentResult).name),
      }),
    }),
    FileNode.extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => <FileNodeView {...props} adapter={adapter} />),
    }).configure({
      resolveSrc,
      toAttrs: (result: UploadResult) => {
        const { url, name, size, mime } = result as AttachmentResult;
        return { src: url, name, size, mime };
      },
    }),
    Video.extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => <VideoNodeView {...props} adapter={adapter} />),
    }).configure({ resolveSrc }),
    Embed.extend({
      addNodeView: () =>
        ReactNodeViewRenderer((props) => (
          <EmbedNodeView {...props} documentId={editing?.documentId} />
        )),
    }).configure({ resolveSrc }),
    Mermaid.extend({ addNodeView: () => ReactNodeViewRenderer(MermaidNodeView) }),
    Mention.extend({
      // eslint-disable-next-line lingui/no-unlocalized-strings -- element name
      addNodeView: () => ReactNodeViewRenderer(MentionNodeView, { as: "span" }),
    }).configure({ items: pages ? withPageMentions({ items }, pages).items : items }),
  ];
}
