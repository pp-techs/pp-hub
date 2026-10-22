// Pure record-store logic: the snapshot an app holds of its node's records,
// and how a `records` event, a write's answer or an optimistic edit changes it.
// No React or bridge, so it is unit tested. (Kept identical in app-kanban and
// app-checklist; the apps share no workspace package.)

import type { AppRecord, RecordsEvent } from "@pp-hub/app-sdk";

/** Every record of the node, by id. Treated as immutable: each change makes a new map. */
export type RecordMap = ReadonlyMap<string, AppRecord>;

export function fromList(list: AppRecord[]): RecordMap {
  return new Map(list.map((r) => [r.id, r]));
}

/**
 * The map with `record` in it. One older than the cached copy (a lower `rev`)
 * is ignored: a write's answer and the event it caused race, and the lower rev
 * is the stale one. An equal rev replaces, which also drops an optimistic edit.
 */
export function upsertRecord(map: RecordMap, record: AppRecord): RecordMap {
  const cached = map.get(record.id);
  if (cached && cached.rev > record.rev) return map;
  const next = new Map(map);
  next.set(record.id, record);
  return next;
}

export function deleteRecord(map: RecordMap, id: string): RecordMap {
  if (!map.has(id)) return map;
  const next = new Map(map);
  next.delete(id);
  return next;
}

/** An optimistic edit: `patch` shallow-merged into the record's data, its rev unchanged. */
export function patchRecord(map: RecordMap, id: string, patch: Record<string, unknown>): RecordMap {
  const cached = map.get(id);
  if (!cached) return map;
  const next = new Map(map);
  next.set(id, { ...cached, data: { ...cached.data, ...patch } });
  return next;
}

export function applyRecordsEvent(map: RecordMap, event: RecordsEvent): RecordMap {
  switch (event.type) {
    case "record.upsert":
      return upsertRecord(map, event.record);
    case "record.delete":
      return deleteRecord(map, event.id);
    default:
      return map;
  }
}

/** The records of one collection in creation order (created_at, then id), as `records.list` gives them. */
export function inCollection(map: RecordMap, collection: string): AppRecord[] {
  const out: AppRecord[] = [];
  for (const r of map.values()) if (r.collection === collection) out.push(r);
  return out.sort(
    (a, b) =>
      // RFC 3339 instants: Date.parse keeps milliseconds, and the server's
      // fractions (trailing zeros trimmed) carry microseconds past that.
      Date.parse(a.created_at) - Date.parse(b.created_at) ||
      subMillis(a.created_at) - subMillis(b.created_at) ||
      cmp(a.id, b.id),
  );
}

/** The fraction digits of an RFC 3339 instant past the millisecond, as a number of nanoseconds. */
function subMillis(instant: string): number {
  const fraction = /\.(\d+)/.exec(instant)?.[1] ?? "";
  return Number(fraction.padEnd(9, "0").slice(3, 9));
}

const cmp = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);
