import { expect, test } from "vite-plus/test";
import { describeSelection, nextSelection, type Selection } from "../src/lib/selection.ts";
import type { Node } from "../src/lib/api/types.ts";

const order = ["a", "b", "c", "d"].map(
  (id) => ({ id, name: id, kind: id === "a" ? "dir" : "file" }) as Node,
);

const state = (ids: string[], anchorId: string | null = null): Selection => ({
  ids: new Set(ids),
  anchorId,
});

const plain = { toggle: false, range: false };

test("a plain click replaces the selection and moves the anchor", () => {
  const next = nextSelection(order, state(["b", "c"], "b"), "d", plain);
  expect([...next.ids]).toEqual(["d"]);
  expect(next.anchorId).toBe("d");
});

test("⌘-click adds and removes without clearing the rest", () => {
  const added = nextSelection(order, state(["b"], "b"), "d", { toggle: true, range: false });
  expect([...added.ids].sort()).toEqual(["b", "d"]);

  const removed = nextSelection(order, added, "b", { toggle: true, range: false });
  expect([...removed.ids]).toEqual(["d"]);
});

test("shift-click takes the inclusive range and keeps the anchor", () => {
  const next = nextSelection(order, state(["b"], "b"), "d", { toggle: false, range: true });
  expect([...next.ids]).toEqual(["b", "c", "d"]);
  expect(next.anchorId).toBe("b");
});

test("shift-click extends backwards too", () => {
  const next = nextSelection(order, state(["c"], "c"), "a", { toggle: false, range: true });
  expect([...next.ids]).toEqual(["a", "b", "c"]);
});

test("toggle wins when both modifiers are held", () => {
  const next = nextSelection(order, state(["b"], "b"), "d", { toggle: true, range: true });
  expect([...next.ids].sort()).toEqual(["b", "d"]);
});

test("shift-click with no anchor, or off the listing, replaces", () => {
  const noAnchor = nextSelection(order, state([]), "c", { toggle: false, range: true });
  expect([...noAnchor.ids]).toEqual(["c"]);

  const staleAnchor = nextSelection(order, state(["c"], "gone"), "c", {
    toggle: false,
    range: true,
  });
  expect([...staleAnchor.ids]).toEqual(["c"]);
});

test.each([
  [[], ""],
  [["a"], "1 folder"],
  [["b"], "1 file"],
  [["b", "c"], "2 files"],
  [["a", "b", "c"], "1 folder and 2 files"],
])("describeSelection(%o) === %s", (ids, expected) => {
  const nodes = order.filter((n) => (ids as string[]).includes(n.id));
  expect(describeSelection(nodes)).toBe(expected);
});
