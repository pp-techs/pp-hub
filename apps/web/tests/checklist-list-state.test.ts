import { expect, test } from "vite-plus/test";
import type { Item, List } from "../../../packages/ext-checklist/src/api.ts";
import { applyEvent, progress } from "../../../packages/ext-checklist/src/list-state.ts";

const item = (id: string, text: string, done = false): Item => ({
  id,
  text,
  done,
  created_at: "2026-01-01T00:00:00Z",
});

const list = (items: Item[]): List => ({
  id: "l",
  name: "List",
  caps: { view: true, download: true, share: true, edit: true, owner: true },
  items,
});

test("an upsert event replaces an item in place and appends a new one", () => {
  const before = list([item("a", "one"), item("b", "two")]);
  const ticked = applyEvent(before, { type: "item.upsert", item: item("a", "one", true) });
  expect(ticked.items.map((i) => [i.id, i.done])).toEqual([
    ["a", true],
    ["b", false],
  ]);
  const added = applyEvent(ticked, { type: "item.upsert", item: item("c", "three") });
  expect(added.items.map((i) => i.id)).toEqual(["a", "b", "c"]);
  // The snapshot it was applied to is left alone.
  expect(before.items[0]!.done).toBe(false);
});

test("a delete event drops the item, and an unknown id changes nothing", () => {
  const before = list([item("a", "one"), item("b", "two")]);
  expect(applyEvent(before, { type: "item.delete", id: "a" }).items.map((i) => i.id)).toEqual([
    "b",
  ]);
  expect(applyEvent(before, { type: "item.delete", id: "zzz" }).items).toHaveLength(2);
});

test("progress counts ticked items against all of them", () => {
  expect(progress([])).toEqual({ done: 0, total: 0 });
  expect(progress([item("a", "x", true), item("b", "y"), item("c", "z", true)])).toEqual({
    done: 2,
    total: 3,
  });
});
