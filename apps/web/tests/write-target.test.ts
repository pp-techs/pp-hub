import { expect, test } from "vite-plus/test";
import { pasteTargets, writeTarget, type ClipboardState } from "../src/lib/write-target.ts";
import type { Node } from "../src/lib/api/types.ts";

const node = (id: string, parent_id?: string): Node =>
  ({ id, name: id, kind: "dir", parent_id }) as Node;

// reports -> quarterly, both under the space root.
const dirs = { reports: { parent: undefined }, quarterly: { parent: "reports" } };

test("a listing inside a folder writes to that folder, not the space root", () => {
  const t = writeTarget({
    spaceId: "s1",
    spaceName: "Acme",
    dir: "reports",
    dirName: "Reports",
    dirCanEdit: true,
  });
  expect(t.dir).toBe("reports");
  expect(t.label).toBe("Acme / Reports");
  expect(t.canWrite).toBe(true);
});

test("a surface with no ?dir= writes to the space root, which any member may do", () => {
  const t = writeTarget({ spaceId: "s1", spaceName: "Acme" });
  expect(t.dir).toBeUndefined();
  expect(t.label).toBe("Acme");
  expect(t.canWrite).toBe(true);
});

test("a folder without edit capability cannot be written to", () => {
  expect(writeTarget({ spaceId: "s1", dir: "reports", dirCanEdit: false }).canWrite).toBe(false);
  // Unresolved capability is not permission.
  expect(writeTarget({ spaceId: "s1", dir: "reports" }).canWrite).toBe(false);
});

test("nothing can be written before a space is known", () => {
  expect(writeTarget({}).canWrite).toBe(false);
});

const root = writeTarget({ spaceId: "s1", spaceName: "Acme" });
const intoQuarterly = writeTarget({
  spaceId: "s1",
  dir: "quarterly",
  dirCanEdit: true,
});
const clip = (mode: ClipboardState["mode"], nodes: Node[]): ClipboardState => ({
  mode,
  spaceId: "s1",
  nodes,
});

test("pasting needs a clipboard, a space and write capability", () => {
  expect(pasteTargets(null, root, dirs)).toEqual([]);
  expect(pasteTargets(clip("copy", []), root, dirs)).toEqual([]);
  expect(
    pasteTargets(clip("copy", [node("f")]), writeTarget({ spaceId: "s1", dir: "reports" }), dirs),
  ).toEqual([]);
});

test("a clipboard cannot cross spaces, because copy and move cannot", () => {
  const foreign: ClipboardState = { mode: "copy", spaceId: "s2", nodes: [node("f")] };
  expect(pasteTargets(foreign, root, dirs)).toEqual([]);
});

test("cutting a folder into itself or its own subtree is refused", () => {
  expect(pasteTargets(clip("cut", [node("quarterly")]), intoQuarterly, dirs)).toEqual([]);
  expect(pasteTargets(clip("cut", [node("reports")]), intoQuarterly, dirs)).toEqual([]);
});

test("cutting into the folder a node already sits in is refused", () => {
  expect(pasteTargets(clip("cut", [node("f", "quarterly")]), intoQuarterly, dirs)).toEqual([]);
});

test("a copy into the folder the original sits in is still a copy", () => {
  const nodes = [node("f", "quarterly")];
  expect(pasteTargets(clip("copy", nodes), intoQuarterly, dirs)).toEqual(nodes);
});

test("one refused node does not refuse the rest", () => {
  const moving = node("f", "reports");
  const landed = pasteTargets(clip("cut", [node("quarterly"), moving]), intoQuarterly, dirs);
  expect(landed).toEqual([moving]);
});
