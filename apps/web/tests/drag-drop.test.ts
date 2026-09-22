import { expect, test } from "vite-plus/test";
import { isDescendantOf, isValidMoveTarget } from "../src/lib/drag-drop.ts";
import type { Node } from "../src/lib/api/types.ts";

const dir = (id: string, parent_id?: string): Node =>
  ({ id, name: id, kind: "dir", parent_id, space_id: "s1" }) as Node;

const file = (id: string, parent_id?: string): Node =>
  ({ id, name: id, kind: "file", parent_id, space_id: "s1" }) as Node;

// Hierarchy:
// root
//   ├── f1 (folder)
//   │    └── f1_1 (folder)
//   │         └── f1_1_1 (folder)
//   ├── f2 (folder)
//   └── file1 (file)
const dirs = {
  f1: { parent: undefined },
  f1_1: { parent: "f1" },
  f1_1_1: { parent: "f1_1" },
  f2: { parent: undefined },
};

test("isDescendantOf checks ancestry properly", () => {
  expect(isDescendantOf("f1", "f1", dirs)).toBe(true);
  expect(isDescendantOf("f1_1", "f1", dirs)).toBe(true);
  expect(isDescendantOf("f1_1_1", "f1", dirs)).toBe(true);
  expect(isDescendantOf("f1_1_1", "f1_1", dirs)).toBe(true);

  expect(isDescendantOf("f2", "f1", dirs)).toBe(false);
  expect(isDescendantOf("f1", "f1_1", dirs)).toBe(false);
  expect(isDescendantOf("f1", "f1_1_1", dirs)).toBe(false);
  expect(isDescendantOf("unknown", "f1", dirs)).toBe(false);
});

test("isDescendantOf safely terminates on cyclic parent references", () => {
  const cyclicDirs = {
    c1: { parent: "c2" },
    c2: { parent: "c1" },
  };
  expect(isDescendantOf("c1", "other", cyclicDirs)).toBe(false);
  expect(isDescendantOf("c1", "c2", cyclicDirs)).toBe(true);
});

test("isValidMoveTarget rejects missing drag state or mismatched space", () => {
  expect(isValidMoveTarget("f2", null, "s1", dirs)).toBe(false);
  expect(isValidMoveTarget("f2", { spaceId: "s1", nodes: [] }, "s1", dirs)).toBe(false);
  expect(isValidMoveTarget("f2", { spaceId: "s2", nodes: [file("file1")] }, "s1", dirs)).toBe(
    false,
  );
});

test("isValidMoveTarget rejects dropping into any dragged node itself", () => {
  expect(isValidMoveTarget("f1", { spaceId: "s1", nodes: [dir("f1")] }, "s1", dirs)).toBe(false);
  expect(
    isValidMoveTarget("f1", { spaceId: "s1", nodes: [file("file1"), dir("f1")] }, "s1", dirs),
  ).toBe(false);
});

test("isValidMoveTarget rejects dropping into a descendant of any dragged directory", () => {
  // Moving f1 into its child f1_1
  expect(isValidMoveTarget("f1_1", { spaceId: "s1", nodes: [dir("f1")] }, "s1", dirs)).toBe(false);

  // Moving f1 into its grandchild f1_1_1
  expect(isValidMoveTarget("f1_1_1", { spaceId: "s1", nodes: [dir("f1")] }, "s1", dirs)).toBe(
    false,
  );

  // Multi-selection with f1 and another node
  expect(
    isValidMoveTarget("f1_1", { spaceId: "s1", nodes: [file("file1"), dir("f1")] }, "s1", dirs),
  ).toBe(false);
});

test("isValidMoveTarget allows moving a folder into a sibling or unrelated folder", () => {
  expect(isValidMoveTarget("f2", { spaceId: "s1", nodes: [dir("f1")] }, "s1", dirs)).toBe(true);
  expect(isValidMoveTarget("f1", { spaceId: "s1", nodes: [dir("f2")] }, "s1", dirs)).toBe(true);
});

test("isValidMoveTarget allows moving child out to ancestor or root", () => {
  // Moving f1_1_1 to f1
  expect(
    isValidMoveTarget("f1", { spaceId: "s1", nodes: [dir("f1_1_1", "f1_1")] }, "s1", dirs),
  ).toBe(true);

  // Moving f1_1 to space root (undefined)
  expect(
    isValidMoveTarget(undefined, { spaceId: "s1", nodes: [dir("f1_1", "f1")] }, "s1", dirs),
  ).toBe(true);
});

test("isValidMoveTarget rejects dropping where all nodes are already located", () => {
  // f1 is already at root (parent_id undefined)
  expect(isValidMoveTarget(undefined, { spaceId: "s1", nodes: [dir("f1")] }, "s1", dirs)).toBe(
    false,
  );

  // f1_1 is already in f1
  expect(isValidMoveTarget("f1", { spaceId: "s1", nodes: [dir("f1_1", "f1")] }, "s1", dirs)).toBe(
    false,
  );
});

test("isValidMoveTarget allows multi-selection where at least one node moves to a new parent", () => {
  // f1_1 is already in f1, but f2 is at root (parent_id undefined)
  expect(
    isValidMoveTarget(
      "f1",
      {
        spaceId: "s1",
        nodes: [dir("f1_1", "f1"), dir("f2")],
      },
      "s1",
      dirs,
    ),
  ).toBe(true);
});
