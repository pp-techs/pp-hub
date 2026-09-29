import type { AnyExtension } from "@tiptap/core";
import { createBlockKit, type BlockKitOptions, type MentionOptions } from "@slash-editor/core";

/**
 * `createMdocKit` options: `createBlockKit`'s, except `mention.items` is
 * optional (a surface without a provider — the read-only renderer, the
 * fixture generator — still needs the `mention` node in its schema).
 *
 * The v1 schema is image, file, video, embed, mermaid, table, columns,
 * mention and the comment mark on top of the baseline set. Passing `false`
 * for one of those is only valid when `extend` supplies a variant of the
 * same node (e.g. `Video.extend({ addNodeView })`); otherwise the schema
 * drifts from what `apps/server/internal/mdoc` stores.
 */
export type MdocKitOptions = Omit<BlockKitOptions, "mention"> & {
  mention?: Partial<MentionOptions> | false;
};

const noMentions: MentionOptions["items"] = () => [];

/**
 * StarterKit ships `underline` (serialized as `++x++`) and `BlockKitOptions`
 * has no key for it, so the starterKit entry is reconfigured without it.
 */
export function withoutUnderline(extensions: AnyExtension[]): AnyExtension[] {
  return extensions.map((extension) =>
    extension.name === "starterKit" ? extension.configure({ underline: false }) : extension,
  );
}

/**
 * `createBlockKit` narrowed to the v1 block set: exactly what
 * `apps/server/internal/mdoc` stores. Keep in sync with the Go golden
 * fixtures (`bun apps/web/scripts/mdoc-fixtures.ts`). `mention: {items}` and
 * `comment: {store}` pass through to slash-editor.
 */
export function createMdocKit(options: MdocKitOptions = {}): AnyExtension[] {
  const { mention, ...rest } = options;
  return withoutUnderline(
    createBlockKit({
      ...rest,
      mention: mention === false ? false : { items: noMentions, ...mention },
    }),
  );
}
