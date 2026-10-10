import { expect, test } from "vite-plus/test";
import {
  buildGridRows,
  gridColumns,
  hitTestGrid,
  hitTestList,
} from "../src/lib/listing-geometry.ts";
import type { Node } from "../src/lib/api/types.ts";

const make = (prefix: string, kind: Node["kind"], count: number) =>
  Array.from(
    { length: count },
    (_, i) => ({ id: `${prefix}${i}`, name: `${prefix}${i}`, kind }) as Node,
  );

const dirs = make("d", "dir", 7);
const files = make("f", "file", 5);

test("gridColumns follows the container breakpoints", () => {
  expect(gridColumns(511)).toBe(2);
  expect(gridColumns(512)).toBe(3);
  expect(gridColumns(671)).toBe(3);
  expect(gridColumns(672)).toBe(4);
  expect(gridColumns(895)).toBe(4);
  expect(gridColumns(896)).toBe(5);
});

test("buildGridRows labels each group and carries the gaps in row heights", () => {
  const { rows, starts, total } = buildGridRows(dirs, files, 3);
  expect(rows.map((r) => r.key)).toEqual([
    "label:dirs",
    "dirs:0",
    "dirs:1",
    "dirs:2",
    "label:files",
    "files:0",
    "files:1",
  ]);
  expect(rows.map((r) => r.kind)).toEqual([
    "label",
    "cards",
    "cards",
    "cards",
    "label",
    "cards",
    "cards",
  ]);
  expect(rows.map((r) => r.height)).toEqual([24, 60, 60, 72, 24, 210, 198]);
  expect(starts).toEqual([0, 24, 84, 144, 216, 240, 450]);
  expect(total).toBe(648);
});

// 600px at 3 columns: 192px columns, a column every 204px.
const layout = buildGridRows(dirs, files, 3);

test("hitTestGrid picks the one card under a small band", () => {
  const ids = hitTestGrid(layout, 3, 600, { left: 250, top: 100, width: 20, height: 10 });
  expect([...ids]).toEqual(["d4"]);
});

test("hitTestGrid finds nothing in the gap between columns", () => {
  const ids = hitTestGrid(layout, 3, 600, { left: 196, top: 100, width: 6, height: 10 });
  expect(ids.size).toBe(0);
});

test("hitTestGrid spans the last folder row and the first file row", () => {
  const ids = hitTestGrid(layout, 3, 600, { left: 0, top: 150, width: 600, height: 120 });
  expect([...ids].sort()).toEqual(["d6", "f0", "f1", "f2"]);
});

const rows = make("r", "file", 5);

test("hitTestList takes every row the band crosses", () => {
  const ids = hitTestList(rows, 36, 500, { left: 10, top: 40, width: 20, height: 70 });
  expect([...ids]).toEqual(["r1", "r2", "r3"]);
});

test("hitTestList finds nothing right of the rows", () => {
  const ids = hitTestList(rows, 36, 500, { left: 510, top: 40, width: 20, height: 70 });
  expect(ids.size).toBe(0);
});

test("hitTestList leaves out a row the band only touches", () => {
  const ids = hitTestList(rows, 36, 500, { left: 10, top: 72, width: 20, height: 36 });
  expect([...ids]).toEqual(["r2"]);
});
