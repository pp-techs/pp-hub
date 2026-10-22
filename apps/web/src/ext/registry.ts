import { useSyncExternalStore } from "react";
import type { Extension } from "@/lib/api";
import { packagedManifest, syncAppStrings } from "./packaged";
import type { ExtManifest, ExtNodeKind } from "./types";

// The apps installed on this server (M51). The SPA carries no code for any of
// them: it learns them from `GET /extensions` at runtime and draws each in a
// sandboxed frame (`packaged.tsx`, `app-frame.tsx`). Every read goes through
// the functions below; a component that draws from them subscribes with
// `useExtRegistry` so an app installed after the first paint is drawn too.

let apps: ExtManifest[] = [];
let kinds = new Map<string, ExtNodeKind>();
let version = 0;
const listeners = new Set<() => void>();

/** A fingerprint of an app list: a refetch that changes nothing changes nothing here. */
let fingerprint = "";

/**
 * Takes the apps out of an extensions listing. Every fetch of the listing
 * calls it (`extensionsOptions`), whichever space it was asked for: each lists
 * every installed app.
 */
export function registerPackagedApps(list: readonly Extension[]): void {
  const next = list
    .map((e) => `${e.id}@${e.package.digest}:${e.name}`)
    .sort()
    .join("|");
  if (next === fingerprint) return;
  fingerprint = next;
  apps = list.map(packagedManifest);
  kinds = new Map(apps.flatMap((e) => e.nodeKinds.map((k) => [k.kind, k] as const)));
  syncAppStrings(list);
  version++;
  for (const l of listeners) l();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/**
 * Re-renders the caller whenever the set of apps changes. Call it in any
 * component that reads `extKindOf` or `extensionById`.
 */
export function useExtRegistry(): number {
  return useSyncExternalStore(
    subscribe,
    () => version,
    () => version,
  );
}

/** The registered kind for a node's `ext_kind`, or undefined when no installed app owns it. */
export function extKindOf(extKind: string | undefined): ExtNodeKind | undefined {
  return extKind === undefined ? undefined : kinds.get(extKind);
}

/** The installed app with this server id, or undefined. */
export function extensionById(id: string): ExtManifest | undefined {
  return apps.find((e) => e.id === id);
}
