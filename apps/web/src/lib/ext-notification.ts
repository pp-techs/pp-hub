import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { ExtManifest, ExtNodeKind } from "@pp-hub/ext-sdk";
import type { AppNotification } from "@/lib/api";

/** `ext:<id>:<verb>` split into its parts; null for the core kinds and anything malformed. */
export function parseExtKind(kind: string): { extId: string; verb: string } | null {
  const [prefix, extId, ...verb] = kind.split(":");
  // eslint-disable-next-line lingui/no-unlocalized-strings -- wire kind prefix
  if (prefix !== "ext" || !extId || verb.length === 0 || !verb[0]) return null;
  return { extId, verb: verb.join(":") };
}

/**
 * The line the bell shows for an extension notification: the manifest's
 * wording for its verb, else a generic one (an extension this build lacks,
 * or a verb it does not word). Null for a core kind.
 */
export function extNotificationMessage(
  item: Pick<AppNotification, "kind" | "actor" | "excerpt" | "node">,
  manifestOf: (extId: string) => ExtManifest | undefined,
): MessageDescriptor | null {
  const parsed = parseExtKind(item.kind);
  if (!parsed) return null;
  const worded = manifestOf(parsed.extId)?.notifications?.[parsed.verb];
  if (worded) return worded({ actor: item.actor.name, excerpt: item.excerpt });
  const actor = item.actor.name;
  const doc = item.node.name;
  return msg`${actor} sent you an update on ${doc}`;
}

/**
 * The icon of the extension a notification came from (its first node kind),
 * for the board chip under the line; undefined for a core kind or an
 * extension this build lacks.
 */
export function extNotificationIcon(
  kind: string,
  manifestOf: (extId: string) => ExtManifest | undefined,
): ExtNodeKind["icon"] | undefined {
  const parsed = parseExtKind(kind);
  return parsed ? manifestOf(parsed.extId)?.nodeKinds[0]?.icon : undefined;
}
