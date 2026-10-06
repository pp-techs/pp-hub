import type { AnyExtension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import {
  createBlockKit,
  type BlockKitOptions,
  type MentionOptions,
  type PageStore,
  type PagesOptions,
} from "@slash-editor/core";
import { CodeBlock, codeBlock, type Lowlight } from "@slash-editor/core/code-block";
import { emoji } from "@slash-editor/core/emoji";

/**
 * `createMdocKit` options: `createBlockKit`'s, except `mention.items` is
 * optional (a surface without a provider — the read-only renderer, the
 * fixture generator — still needs the `mention` node in its schema), and
 * `pages` defaults to a store that knows no page, so `subPage` and `pageLink`
 * are always in the schema.
 *
 * The v1 schema is image, file, video, embed, mermaid, table, columns,
 * mention, emoji, subPage, pageLink and the comment mark on top of the
 * baseline set. Passing `false` for one of those is only valid when `extend`
 * supplies a variant of the same node (e.g. `Video.extend({ addNodeView })`);
 * otherwise the schema drifts from what `apps/server/internal/mdoc` stores.
 */
export type MdocKitOptions = Omit<BlockKitOptions, "mention" | "pages"> & {
  mention?: Partial<MentionOptions> | false;
  pages?: PagesOptions;
};

const noMentions: MentionOptions["items"] = () => [];

/** Where a page lives in pp-hub, and what its markdown link points at. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
export const pageHref = (pageId: string) => `/edit/${pageId}`;

/**
 * A store that knows no page: titles read "Untitled", nothing is created.
 * Also what a page node view falls back to in an editor built without one.
 */
export const emptyPageStore: PageStore = {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- developer-facing error
  create: () => Promise.reject(new Error("no page store")),
  update: () => {},
  peek: () => undefined,
  load: async () => undefined,
  listChildren: () => [],
  search: () => [],
  subscribe: () => () => {},
};

const readOnlyPages: PagesOptions = {
  store: emptyPageStore,
  currentPageId: null,
  onNavigate: () => {},
  resolveHref: pageHref,
};

/**
 * The emoji node without Tiptap's glyph-to-node conversion. That plugin
 * rewrites every emoji glyph in a changed range into a node — including the
 * ranges a Yjs sync or a server reseed changes — so every peer joining a
 * room would convert the same glyphs at once (duplicated nodes) and a
 * document's `🎉` would silently become `:tada:` under whoever opened it. A
 * typed or pasted glyph stays text, as the markdown wrote it; the `:` picker
 * and `:shortcode:` input still insert the node.
 */
const mdocEmoji = emoji().extend({
  addProseMirrorPlugins() {
    return (this.parent?.() ?? []).map((plugin) =>
      plugin.spec.appendTransaction
        ? new Plugin({ ...plugin.spec, appendTransaction: undefined })
        : plugin,
    );
  },
});

/**
 * The syntax-highlighted code block (lowlight's `common` grammars). It keeps
 * the `codeBlock` node name, `language` attribute and markdown fence, so the
 * stored document is unchanged; highlighting is decoration only, themed by the
 * `hljs-*` rules in `globals.css`.
 *
 * A block without a language is left plain: lowlight would otherwise guess
 * one, and prose or logs come out colored as if they were code. (Not
 * `defaultLanguage`, which would also become the stored `language` of every
 * new block.)
 */
export const mdocLowlight: Lowlight = CodeBlock.options.lowlight;
// eslint-disable-next-line lingui/no-unlocalized-strings -- highlight.js language id
mdocLowlight.highlightAuto = (value: string) => mdocLowlight.highlight("plaintext", value);
const mdocCodeBlock = codeBlock({ lowlight: mdocLowlight });

/**
 * `createBlockKit` narrowed to the v1 block set: exactly what
 * `apps/server/internal/mdoc` stores. Keep in sync with the Go golden
 * fixtures (`bun apps/web/scripts/mdoc-fixtures.ts`). `mention: {items}` and
 * `comment: {store}` pass through to slash-editor.
 *
 * StarterKit's `underline` is off: it serializes as `++x++`, which `mdoc`
 * does not store.
 */
export function createMdocKit(options: MdocKitOptions = {}): AnyExtension[] {
  const { mention, starterKit, pages, ...rest } = options;
  return createBlockKit({
    ...rest,
    starterKit: { ...starterKit, underline: false },
    codeBlock: rest.codeBlock ?? mdocCodeBlock,
    emoji: rest.emoji ?? mdocEmoji,
    mention: mention === false ? false : { items: noMentions, ...mention },
    pages: { resolveHref: pageHref, ...(pages ?? readOnlyPages) },
  });
}
