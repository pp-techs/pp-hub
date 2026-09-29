import { Extension, type AnyExtension } from "@tiptap/core";
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
 * ⌘K opens the link popover — the keyboard half of the bubble toolbar's Link
 * button, which runs the same `openLinkEditor` command. The mark keys (⌘B,
 * ⌘I, ⌘E, ⌘⇧S) travel with the Tiptap marks themselves; the link editor is
 * slash-editor state and shipped without a keymap, so the tooltip's ⌘K hint
 * used to have nothing behind it. `openLinkEditor` reports failure where no
 * link can be started (empty selection, cursor outside a link), and the key
 * then falls through untouched.
 */
const linkShortcut = Extension.create({
  name: "linkShortcut",
  addKeyboardShortcuts() {
    return {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- Tiptap keymap id
      "Mod-k": ({ editor }) => editor.commands.openLinkEditor(),
    };
  },
});

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
      // The caller's `extend` wins nothing and loses nothing: the shortcut is
      // appended behind it.
      extend: [...(rest.extend ?? []), linkShortcut],
      mention: mention === false ? false : { items: noMentions, ...mention },
    }),
  );
}
