import type { Extension } from "@/lib/api";
import { extensionById } from "@/ext/registry";

/**
 * The apps the rail's Apps section lists: on for the space, and carried by
 * this build. An app the server has on but the SPA does not register has no
 * editor to open its nodes in, so it is not offered. `isRegistered` is a seam
 * for tests; the default asks the registry.
 */
export function sidebarApps(
  extensions: readonly Extension[] | undefined,
  isRegistered: (id: string) => boolean = (id) => extensionById(id) !== undefined,
): Extension[] {
  return (extensions ?? []).filter((e) => e.enabled && isRegistered(e.id));
}
