import type { ExtManifest, ExtNodeKind } from "@pp-hub/ext-sdk";
import { extensions } from "./registry.gen";

// The extensions compiled into this build come from `pp-apps.json` through
// `registry.gen.ts`; the server's `extensions_gen.go` is the other half.
export { extensions };

const kinds = new Map<string, ExtNodeKind>(
  extensions.flatMap((e) => e.nodeKinds.map((k) => [k.kind, k] as const)),
);

/** The registered kind for a node's `ext_kind`, or undefined when this build does not carry it. */
export function extKindOf(extKind: string | undefined): ExtNodeKind | undefined {
  return extKind === undefined ? undefined : kinds.get(extKind);
}

/** The compiled-in extension with this server id, or undefined when this build does not carry it. */
export function extensionById(id: string): ExtManifest | undefined {
  return extensions.find((e) => e.id === id);
}
