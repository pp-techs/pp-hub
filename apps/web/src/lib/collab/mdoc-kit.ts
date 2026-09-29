import type { AnyExtension } from "@tiptap/core";
import { createBlockKit } from "@slash-editor/core";

type BlockKitOptions = NonNullable<Parameters<typeof createBlockKit>[0]>;

/**
 * The v1 block set: exactly what `apps/server/internal/mdoc` stores. Keep in
 * sync with the Go golden fixtures (`bun apps/web/scripts/mdoc-fixtures.ts`).
 */
export const MDOC_KIT_OPTIONS = {
  video: false,
  embed: false,
  comment: false,
} as const satisfies BlockKitOptions;

/**
 * StarterKit ships `underline` (serialized as `++x++`) and `BlockKitOptions`
 * has no key for it, so the starterKit entry is reconfigured without it.
 */
export function withoutUnderline(extensions: AnyExtension[]): AnyExtension[] {
  return extensions.map((extension) =>
    extension.name === "starterKit" ? extension.configure({ underline: false }) : extension,
  );
}

/** `createBlockKit` narrowed to the v1 block set; `options` add editor wiring. */
export function createMdocKit(options: BlockKitOptions = {}): AnyExtension[] {
  return withoutUnderline(createBlockKit({ ...options, ...MDOC_KIT_OPTIONS }));
}
