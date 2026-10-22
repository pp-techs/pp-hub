import type { Messages } from "@lingui/core";

// `@lingui/vite-plugin` turns the sibling `messages.po` into an ES module of
// pre-compiled messages during Vite's transform step. TypeScript resolves an
// arbitrary extension through a `.d.<ext>.ts` sibling (tsconfig's
// `allowArbitraryExtensions`), not through a `declare module "*.po"` wildcard,
// so a new locale needs a copy of this file beside its catalog.
export declare const messages: Messages;
