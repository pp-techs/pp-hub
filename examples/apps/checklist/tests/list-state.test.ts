import { expect, test } from "vite-plus/test";
import type { AppRecord } from "@pp-hub/app-sdk";
import { applyRecordsEvent, fromList } from "../src/bridge/records.ts";
import { cleanText, itemsOf, progress } from "../src/list-state.ts";

// Ids "a", "b", … are created in that order, a second apart.
const createdAt = (id: string) => `2026-01-01T00:00:${10 + "abcdefgh".indexOf(id)}Z`;

const item = (id: string, text: string, done = false, rev = 1): AppRecord => ({
  id,
  collection: "items",
  data: { text, done },
  rev,
  created_by: null,
  updated_by: null,
  created_at: createdAt(id),
  updated_at: createdAt(id),
});

test("an upsert event replaces an item in place and appends a new one", () => {
  const before = fromList([item("a", "one"), item("b", "two")]);
  const ticked = applyRecordsEvent(before, {
    type: "record.upsert",
    record: item("a", "one", true, 2),
  });
  expect(itemsOf(ticked).map((i) => [i.id, i.done])).toEqual([
    ["a", true],
    ["b", false],
  ]);
  const added = applyRecordsEvent(ticked, { type: "record.upsert", record: item("c", "three") });
  expect(itemsOf(added).map((i) => i.id)).toEqual(["a", "b", "c"]);
  // The snapshot it was applied to is left alone.
  expect(itemsOf(before)[0]!.done).toBe(false);
});

test("a delete event drops the item, and an unknown id changes nothing", () => {
  const before = fromList([item("a", "one"), item("b", "two")]);
  const del = (id: string) =>
    itemsOf(applyRecordsEvent(before, { type: "record.delete", id, collection: "items" }));
  expect(del("a").map((i) => i.id)).toEqual(["b"]);
  expect(del("zzz")).toHaveLength(2);
});

test("progress counts ticked items against all of them", () => {
  expect(progress([])).toEqual({ done: 0, total: 0 });
  const items = itemsOf(fromList([item("a", "x", true), item("b", "y"), item("c", "z", true)]));
  expect(progress(items)).toEqual({ done: 2, total: 3 });
});

test("records of other collections and malformed data are read safely", () => {
  const other: AppRecord = { ...item("d", "x"), collection: "notes" };
  const broken: AppRecord = { ...item("e", "x"), data: { text: 5, done: "yes" } };
  expect(itemsOf(fromList([other, broken]))).toEqual([{ id: "e", rev: 1, text: "", done: false }]);
});

test("item text is trimmed and holds 1 to 500 characters, as the server required", () => {
  expect(cleanText("  milk  ")).toBe("milk");
  expect(cleanText("   ")).toBeNull();
  expect(cleanText("x".repeat(500))).toHaveLength(500);
  expect(cleanText("x".repeat(501))).toBeNull();
  // Characters, not UTF-16 units: 500 emoji are 1000 units and still fit.
  expect(cleanText("😀".repeat(500))).not.toBeNull();
});
