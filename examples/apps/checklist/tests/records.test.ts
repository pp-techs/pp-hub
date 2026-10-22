import { expect, test } from "vite-plus/test";
import type { AppRecord } from "@pp-hub/app-sdk";
import {
  applyRecordsEvent,
  deleteRecord,
  fromList,
  inCollection,
  patchRecord,
  upsertRecord,
} from "../src/bridge/records.ts";

const rec = (id: string, rev = 1, created_at = "2026-01-01T00:00:00Z"): AppRecord => ({
  id,
  collection: "things",
  data: { n: rev },
  rev,
  created_by: null,
  updated_by: null,
  created_at,
  updated_at: created_at,
});

test("an upsert older than the cached record is ignored; an equal rev replaces", () => {
  const map = fromList([rec("a", 5)]);
  expect(upsertRecord(map, rec("a", 4))).toBe(map);
  const same = upsertRecord(patchRecord(map, "a", { n: 99 }), rec("a", 5));
  expect(same.get("a")?.data).toEqual({ n: 5 });
  expect(upsertRecord(map, rec("a", 6)).get("a")?.rev).toBe(6);
});

test("a patch merges data without touching rev, and a delete of an unknown id is a no-op", () => {
  const map = fromList([rec("a", 2)]);
  const patched = patchRecord(map, "a", { extra: true });
  expect(patched.get("a")).toMatchObject({ rev: 2, data: { n: 2, extra: true } });
  expect(patchRecord(map, "zz", { x: 1 })).toBe(map);
  expect(deleteRecord(map, "zz")).toBe(map);
  expect(
    applyRecordsEvent(map, { type: "record.delete", id: "a", collection: "things" }).size,
  ).toBe(0);
});

test("a collection lists in creation order, instants first, then ids", () => {
  const map = fromList([
    rec("c", 1, "2026-01-01T00:00:05.12Z"),
    rec("b", 1, "2026-01-01T00:00:05.1Z"),
    rec("a", 1, "2026-01-01T00:00:06Z"),
    rec("d", 1, "2026-01-01T00:00:06Z"),
    { ...rec("x"), collection: "other" },
  ]);
  expect(inCollection(map, "things").map((r) => r.id)).toEqual(["b", "c", "a", "d"]);
});

test("instants within one millisecond order by their microseconds, trailing zeros trimmed", () => {
  const map = fromList([
    rec("third", 1, "2026-10-22T08:47:46.265272Z"),
    rec("first", 1, "2026-10-22T08:47:46.26527Z"),
    rec("second", 1, "2026-10-22T08:47:46.265271Z"),
  ]);
  expect(inCollection(map, "things").map((r) => r.id)).toEqual(["first", "second", "third"]);
});
