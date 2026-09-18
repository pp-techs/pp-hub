import { create } from "zustand";
import { ApiError, createFolder, getChildren, uploadFileWithProgress } from "@/lib/api";

export type TransferStatus = "queued" | "uploading" | "done" | "error" | "cancelled";

export interface Transfer {
  id: string;
  /** File name, without any folder prefix. */
  name: string;
  /** Folder path inside the dropped tree, "" for a file dropped on its own. */
  path: string;
  size: number;
  loaded: number;
  status: TransferStatus;
  error?: string;
  spaceId: string;
  /** Destination the drop targeted. Folder segments are created under it. */
  rootParentId?: string;
  /** Wall-clock ms the transfer has spent uploading, for the speed readout. */
  startedAt?: number;
}

/** A file plus where it sat in a dropped folder tree. */
export interface UploadItem {
  file: File;
  path: string;
}

/**
 * How many files upload at once. Browsers cap same-origin connections around
 * six; three leaves room for the listing refetches that follow each success.
 */
const MAX_CONCURRENT = 3;

interface UploadState {
  transfers: Transfer[];
  /** Paused stops new transfers starting; in-flight ones run to completion. */
  paused: boolean;
  collapsed: boolean;
  /** Tray visibility. Dismissing does not cancel anything still running. */
  dismissed: boolean;

  enqueue: (items: UploadItem[], spaceId: string, rootParentId?: string) => void;
  cancel: (id: string) => void;
  retry: (id: string) => void;
  clearCompleted: () => void;
  setPaused: (paused: boolean) => void;
  setCollapsed: (collapsed: boolean) => void;
  dismiss: () => void;
}

/**
 * Live XHR aborts, keyed by transfer id. These are not state — re-rendering on
 * every byte of a 2 GB upload would be a bad trade — so they live beside the
 * store and are looked up on cancel.
 */
const aborts = new Map<string, () => void>();

/**
 * Folder ids created for this session's uploads, keyed by
 * `space:rootParent:path`. Uploading a 400-file folder must not issue 400
 * mkdir calls for the same directory, and two files in the same folder start
 * concurrently — so the cache holds the in-flight promise, not just the result.
 */
const dirCache = new Map<string, Promise<string>>();

/**
 * Callbacks fired whenever an upload changes a folder's contents — a file
 * landing in it, or a folder segment being created inside it — so the data
 * layer can refetch that listing. The store has no access to the query client,
 * and wiring one in would make it unusable outside React.
 */
type UploadListener = (spaceId: string, parentId: string | undefined) => void;
const listeners = new Set<UploadListener>();

export function onUploadSettled(fn: UploadListener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function notify(spaceId: string, parentId: string | undefined): void {
  for (const fn of listeners) fn(spaceId, parentId);
}

let seq = 0;
/**
 * Each transfer's slot in the `transfers` array. Lookups used to scan the
 * array per file and per progress tick, which made large drops quadratic;
 * every mutation below keeps this in sync instead.
 */
const positions = new Map<string, number>();

export const useUploadStore = create<UploadState>()((set, get) => ({
  transfers: [],
  paused: false,
  collapsed: false,
  dismissed: false,

  enqueue: (items, spaceId, rootParentId) => {
    if (items.length === 0) return;
    const added: Transfer[] = items.map(({ file, path }) => ({
      id: `t${++seq}`,
      name: file.name,
      path,
      size: file.size,
      loaded: 0,
      status: "queued",
      spaceId,
      rootParentId,
    }));
    files.set(
      added.map((t) => t.id),
      items.map((i) => i.file),
    );
    set((s) => {
      added.forEach((t, i) => positions.set(t.id, s.transfers.length + i));
      return { transfers: [...s.transfers, ...added], dismissed: false };
    });
    pump();
  },

  cancel: (id) => {
    aborts.get(id)?.();
    aborts.delete(id);
    pendingTicks.delete(id);
    const found = transferAt(id);
    if (found && (found.transfer.status === "queued" || found.transfer.status === "uploading")) {
      set({
        transfers: replaceAt(get().transfers, found.index, {
          ...found.transfer,
          status: "cancelled",
        }),
      });
    }
    pump();
  },

  retry: (id) => {
    pendingTicks.delete(id);
    const found = transferAt(id);
    if (found) {
      set({
        transfers: replaceAt(get().transfers, found.index, {
          ...found.transfer,
          status: "queued",
          loaded: 0,
          error: undefined,
        }),
      });
    }
    pump();
  },

  clearCompleted: () => {
    const kept = get().transfers.filter(
      (t) => t.status === "queued" || t.status === "uploading" || t.status === "error",
    );
    files.prune(new Set(kept.map((t) => t.id)));
    positions.clear();
    kept.forEach((t, i) => positions.set(t.id, i));
    set({ transfers: kept });
  },

  setPaused: (paused) => {
    set({ paused });
    if (!paused) pump();
  },

  setCollapsed: (collapsed) => set({ collapsed }),

  dismiss: () => {
    files.clear();
    dirCache.clear();
    positions.clear();
    pendingTicks.clear();
    set({ transfers: [], dismissed: true, paused: false });
  },
}));

/**
 * The File objects themselves, kept out of the store so a progress tick never
 * has to copy them.
 */
const files = {
  map: new Map<string, File>(),
  set(ids: string[], list: File[]) {
    ids.forEach((id, i) => this.map.set(id, list[i]));
  },
  get(id: string) {
    return this.map.get(id);
  },
  prune(keep: Set<string>) {
    // Deleting the current entry mid-iteration is well defined for a Map.
    for (const id of this.map.keys()) if (!keep.has(id)) this.map.delete(id);
  },
  clear() {
    this.map.clear();
  },
};

/** The transfer at its indexed slot, or undefined when the id is gone. */
function transferAt(id: string): { index: number; transfer: Transfer } | undefined {
  const { transfers } = useUploadStore.getState();
  const index = positions.get(id);
  if (index === undefined) return undefined;
  const transfer = transfers[index];
  if (!transfer || transfer.id !== id) return undefined;
  return { index, transfer };
}

function replaceAt(transfers: Transfer[], index: number, transfer: Transfer): Transfer[] {
  const next = transfers.slice();
  next[index] = transfer;
  return next;
}

/** Starts queued transfers up to the concurrency limit. */
function pump(): void {
  const { transfers, paused } = useUploadStore.getState();
  if (paused) return;

  let active = 0;
  const queued: Transfer[] = [];
  for (const t of transfers) {
    if (t.status === "uploading") active += 1;
    else if (t.status === "queued") queued.push(t);
  }
  for (const transfer of queued.slice(0, MAX_CONCURRENT - active)) void run(transfer.id);
}

/**
 * Progress ticks buffered since the last flush, coalesced per transfer. XHR
 * progress fires far faster than the screen refreshes, and every write copies
 * the transfers array — so ticks land once per frame, in a single write.
 */
const pendingTicks = new Map<string, Partial<Transfer>>();
let tickFrame: number | null = null;

function flushTicks(): void {
  tickFrame = null;
  if (pendingTicks.size === 0) return;
  const batch = Array.from(pendingTicks);
  pendingTicks.clear();
  const { transfers } = useUploadStore.getState();
  let next: Transfer[] | null = null;
  for (const [id, fields] of batch) {
    const index = positions.get(id);
    if (index === undefined) continue;
    const transfer = transfers[index];
    // A terminal patch since the tick was buffered supersedes it.
    if (!transfer || transfer.id !== id || transfer.status !== "uploading") continue;
    next ??= transfers.slice();
    next[index] = { ...transfer, ...fields };
  }
  if (next) useUploadStore.setState({ transfers: next });
}

function patch(id: string, fields: Partial<Transfer>): void {
  // Byte counts coalesce per frame; structural changes land immediately.
  if (fields.status === undefined) {
    pendingTicks.set(id, { ...pendingTicks.get(id), ...fields });
    if (tickFrame === null) tickFrame = requestAnimationFrame(flushTicks);
    return;
  }
  pendingTicks.delete(id);
  const found = transferAt(id);
  if (!found) return;
  useUploadStore.setState({
    transfers: replaceAt(useUploadStore.getState().transfers, found.index, {
      ...found.transfer,
      ...fields,
    }),
  });
}

async function run(id: string): Promise<void> {
  const file = files.get(id);
  const found = transferAt(id);
  if (!found || found.transfer.status !== "queued" || !file) return;
  const { transfer } = found;

  patch(id, { status: "uploading", startedAt: Date.now(), loaded: 0 });

  try {
    const parentId = await ensureDir(transfer.spaceId, transfer.rootParentId, transfer.path);

    // Cancelling while the folder was being created should not then upload.
    if (transferAt(id)?.transfer.status !== "uploading") {
      return;
    }

    const { promise, abort } = uploadFileWithProgress(transfer.spaceId, file, parentId, (loaded) =>
      patch(id, { loaded }),
    );
    aborts.set(id, abort);
    await promise;
    patch(id, { status: "done", loaded: transfer.size });
    notify(transfer.spaceId, parentId);
  } catch (e) {
    // A cancel already moved the row to its final state; do not overwrite it.
    if (transferAt(id)?.transfer.status === "cancelled") return;
    patch(id, { status: "error", error: describe(e) });
  } finally {
    aborts.delete(id);
    // Folder ids are only valid while the tree they name still exists. Holding
    // them past the end of a batch would make a re-upload of a folder that was
    // deleted in between target a node that is gone.
    if (
      !useUploadStore
        .getState()
        .transfers.some((t) => t.status === "uploading" || t.status === "queued")
    ) {
      dirCache.clear();
    }
    pump();
  }
}

/**
 * Resolves (creating if needed) the folder a relative path points at, one
 * segment at a time under `rootParentId`.
 *
 * A folder that already exists comes back as a 409 from mkdir rather than the
 * existing node, so a conflict falls through to a lookup. That also makes a
 * second upload into the same folder resolve to the folder already there.
 */
async function ensureDir(
  spaceId: string,
  rootParentId: string | undefined,
  path: string,
): Promise<string | undefined> {
  if (!path) return rootParentId;

  let parent = rootParentId;
  let walked = "";
  for (const segment of path.split("/").filter(Boolean)) {
    walked = walked ? `${walked}/${segment}` : segment;
    const key = `${spaceId}:${rootParentId ?? ""}:${walked}`;
    let pending = dirCache.get(key);
    if (!pending) {
      pending = resolveDir(spaceId, parent, segment);
      dirCache.set(key, pending);
    }
    try {
      parent = await pending;
    } catch (e) {
      // A failed mkdir must not poison the cache for a later retry.
      dirCache.delete(key);
      throw e;
    }
  }
  return parent;
}

async function resolveDir(
  spaceId: string,
  parentId: string | undefined,
  name: string,
): Promise<string> {
  try {
    const dir = await createFolder(spaceId, name, parentId);
    // The folder that contains it just gained a row. Without this, uploading a
    // folder into the view you are looking at leaves that view unchanged until
    // a reload — every later notification names a folder deeper in the tree,
    // not the one on screen.
    notify(spaceId, parentId);
    return dir.id;
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 409) throw e;
    const siblings = await getChildren(spaceId, parentId);
    const existing = siblings.find((n) => n.kind === "dir" && n.name === name);
    if (!existing) throw e;
    return existing.id;
  }
}

function describe(e: unknown): string {
  if (e instanceof ApiError) {
    return e.status === 0 ? e.message : `${e.message || "Upload failed"} (${e.status})`;
  }
  return e instanceof Error ? e.message : "Upload failed";
}
