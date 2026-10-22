// An in-memory host for the ops tests: a record store with revs and 409s, a
// link resolver, and a log of notifications. Only what `ops.ts` calls.

import type { App, AppRecord, LinkTarget, Notification } from "@pp-hub/app-sdk";
import { AppError } from "@pp-hub/app-sdk";
import { boardOf, type Board } from "../src/board-state.ts";
import { fromList, type RecordMap } from "../src/bridge/records.ts";

export interface FakeHost {
  app: App;
  /** The host's records, as `records.list` would give them. */
  records: () => RecordMap;
  board: () => Board;
  notified: Notification[];
  /** Nodes `links.resolve` answers for. */
  linkable: LinkTarget[];
  /** Counts of writes by method. */
  writes: Record<string, number>;
}

export function fakeHost(user = "me"): FakeHost {
  const store = new Map<string, AppRecord>();
  let seq = 0;
  const now = () => new Date(Date.UTC(2026, 0, 1, 0, 0, seq++)).toISOString();
  const host: FakeHost = {
    records: () => fromList([...store.values()]),
    board: () => boardOf(host.records()),
    notified: [],
    linkable: [],
    writes: {},
    app: undefined as unknown as App,
  };
  const count = (m: string) => (host.writes[m] = (host.writes[m] ?? 0) + 1);
  const get = (id: string) => {
    const r = store.get(id);
    if (!r) throw new AppError(404, "not found");
    return r;
  };
  const write = (id: string, data: Record<string, unknown>, ifRev?: number) => {
    const r = get(id);
    if (ifRev !== undefined && ifRev !== r.rev) throw new AppError(409, "rev");
    const next = { ...r, data, rev: r.rev + 1, updated_at: now(), updated_by: user };
    store.set(id, next);
    return structuredClone(next);
  };
  const app = {
    context: {
      protocol: 1,
      app: { id: "kanban", version: "2.0.0" },
      node: { id: "board", name: "Board", space_id: "space", ext_kind: "kanban.board" },
      user: { id: user, name: "Me", email: "me@example.com" },
      caps: { view: true, download: true, share: true, edit: true, owner: true },
      locale: "en",
      theme: { mode: "light", vars: {} },
    },
    records: {
      list: async () => [...store.values()].map((r) => structuredClone(r)),
      get: async (id: string) => structuredClone(get(id)),
      create: async (collection: string, data: object) => {
        count("create");
        const at = now();
        const r: AppRecord = {
          id: `r${seq}`,
          collection,
          data: structuredClone(data) as Record<string, unknown>,
          rev: 1,
          created_by: user,
          updated_by: user,
          created_at: at,
          updated_at: at,
        };
        store.set(r.id, r);
        return structuredClone(r);
      },
      update: async (id: string, data: object, opts?: { ifRev?: number }) => {
        count("update");
        return write(id, structuredClone(data) as Record<string, unknown>, opts?.ifRev);
      },
      merge: async (id: string, patch: object, opts?: { ifRev?: number }) => {
        count("merge");
        return write(id, { ...get(id).data, ...structuredClone(patch) }, opts?.ifRev);
      },
      delete: async (id: string) => {
        count("delete");
        get(id);
        store.delete(id);
      },
    },
    links: {
      resolve: async (ids: string[]) => host.linkable.filter((t) => ids.includes(t.id)),
    },
    notify: async (n: Notification) => {
      host.notified.push(n);
    },
  };
  host.app = app as unknown as App;
  return host;
}

/** A store that ignores optimistic edits: the tests read the host's records. */
export const noStore = { upsert: () => {}, remove: () => {}, patch: () => {} };
