import type { I18n } from "@lingui/core";
import type { Extension } from "@/lib/api";
import { extensions } from "@/ext/registry";

/**
 * The extension's name in the UI's language when this build registers it,
 * else the name the server sent (an extension the SPA does not carry).
 */
export function extensionName(ext: Extension, i18n: I18n): string {
  const registered = extensions.find((e) => e.id === ext.id);
  return registered ? i18n._(registered.name) : ext.name;
}
