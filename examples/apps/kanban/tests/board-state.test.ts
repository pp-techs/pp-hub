import { expect, test } from "vite-plus/test";
import type { AppRecord } from "@pp-hub/app-sdk";
import {
  type Board,
  boardOf,
  type Card,
  cardsIn,
  columnDropAfter,
  dropPosition,
  dueFromInput,
  dueToInput,
  isFiltering,
  isNoopCardMove,
  isNoopColumnMove,
  isOverLimit,
  isOverdue,
  matchesFilter,
  moveCardLocal,
  moveColumnLocal,
  NO_FILTER,
  placeAfter,
  slotFromPointer,
  sortedColumns,
  UNASSIGNED,
} from "../src/board-state.ts";
import { applyRecordsEvent, fromList, type RecordMap } from "../src/bridge/records.ts";
import { between } from "../src/position.ts";

const record = (
  id: string,
  collection: string,
  data: Record<string, unknown>,
  rev = 1,
): AppRecord => ({
  id,
  collection,
  data,
  rev,
  created_by: null,
  updated_by: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
});

const col = (id: string, position: string, wip_limit: number | null = null) =>
  record(id, "columns", { title: id, position, wip_limit });

const cardRec = (id: string, column_id: string, position: string, rev = 1, extra = {}) =>
  record(
    id,
    "cards",
    {
      column_id,
      title: id,
      description_md: "",
      position,
      due_at: null,
      assignees: [],
      label_ids: [],
      link_ids: [],
      ...extra,
    },
    rev,
  );

const label = (id: string, name: string, color: string) => record(id, "labels", { name, color });

const records = (): RecordMap =>
  fromList([
    col("todo", "1i"),
    col("doing", "2i"),
    col("done", "3i"),
    cardRec("a", "todo", "1i"),
    cardRec("b", "todo", "2i"),
    cardRec("c", "todo", "3i"),
    cardRec("d", "doing", "1i"),
    label("l1", "Bug", "#ef4444"),
    label("l2", "Idea", "#3b82f6"),
  ]);

const board = (): Board => boardOf(records());

const card = (id: string, column_id: string, position: string): Card =>
  boardOf(fromList([col(column_id, "1"), cardRec(id, column_id, position)])).cards[0]!;

const ids = (b: Board, column: string) => cardsIn(b, column).map((c) => c.id);

test("between sorts strictly between its neighbours, however often it is halved", () => {
  expect(between("", "")).toBe("i");
  let lo = "1i";
  const hi = "2i";
  for (let n = 0; n < 40; n++) {
    const mid = between(lo, hi);
    expect(mid > lo && mid < hi).toBe(true);
    lo = mid;
  }
  const first = between("", "1i");
  expect(first < "1i").toBe(true);
  expect(between("1i", "") > "1i").toBe(true);
  expect(between("z", "").startsWith("z")).toBe(true);
});

test("slotFromPointer counts the items whose midpoint is above the pointer", () => {
  const mids = [10, 30, 50];
  expect(slotFromPointer(mids, 0)).toBe(0);
  expect(slotFromPointer(mids, 20)).toBe(1);
  expect(slotFromPointer(mids, 40)).toBe(2);
  expect(slotFromPointer(mids, 99)).toBe(3);
  expect(slotFromPointer([], 5)).toBe(0);
});

test("dropPosition into another column names the card above the gap", () => {
  const todo = ["a", "b", "c"];
  expect(dropPosition("todo", todo, 0, "x")).toEqual({ column_id: "todo", after_id: "" });
  expect(dropPosition("todo", todo, 2, "x")).toEqual({ column_id: "todo", after_id: "b" });
  expect(dropPosition("todo", todo, 3, "x")).toEqual({ column_id: "todo", after_id: "c" });
  expect(dropPosition("done", [], 0, "x")).toEqual({ column_id: "done", after_id: "" });
});

test("dropPosition within a column ignores the moving card's own place", () => {
  const todo = ["a", "b", "c"];
  // a dragged to the gap before c lands after b
  expect(dropPosition("todo", todo, 2, "a")).toEqual({ column_id: "todo", after_id: "b" });
  // a dragged to the bottom lands after c
  expect(dropPosition("todo", todo, 3, "a")).toEqual({ column_id: "todo", after_id: "c" });
  // c dragged to the top
  expect(dropPosition("todo", todo, 0, "c")).toEqual({ column_id: "todo", after_id: "" });
  // b dragged to the gap just below a: it stays after a
  expect(dropPosition("todo", todo, 1, "b")).toEqual({ column_id: "todo", after_id: "a" });
  // b dragged to the gap just above itself: also after a
  expect(dropPosition("todo", todo, 2, "b")).toEqual({ column_id: "todo", after_id: "a" });
});

test("isNoopCardMove recognises a drop that leaves the card in place", () => {
  const b = board();
  expect(isNoopCardMove(b, "b", { column_id: "todo", after_id: "a" })).toBe(true);
  expect(isNoopCardMove(b, "a", { column_id: "todo", after_id: "" })).toBe(true);
  expect(isNoopCardMove(b, "a", { column_id: "todo", after_id: "b" })).toBe(false);
  expect(isNoopCardMove(b, "a", { column_id: "doing", after_id: "" })).toBe(false);
});

test("columnDropAfter maps a slot to after_id, null meaning first", () => {
  const cols = ["todo", "doing", "done"];
  expect(columnDropAfter(cols, 0, "done")).toBeNull();
  expect(columnDropAfter(cols, 1, "done")).toBe("todo");
  expect(columnDropAfter(cols, 3, "todo")).toBe("done");
  // todo dragged to the gap just after itself stays first
  expect(columnDropAfter(cols, 1, "todo")).toBeNull();
  expect(isNoopColumnMove(board(), "todo", null)).toBe(true);
  expect(isNoopColumnMove(board(), "doing", "todo")).toBe(true);
  expect(isNoopColumnMove(board(), "doing", "done")).toBe(false);
});

test("moveCardLocal places a card between neighbours in another column and within its own", () => {
  const moved = moveCardLocal(board(), "d", "todo", "a");
  expect(ids(moved, "todo")).toEqual(["a", "d", "b", "c"]);
  expect(ids(moved, "doing")).toEqual([]);

  const top = moveCardLocal(board(), "c", "todo", "");
  expect(ids(top, "todo")).toEqual(["c", "a", "b"]);

  const bottom = moveCardLocal(board(), "a", "todo", "c");
  expect(ids(bottom, "todo")).toEqual(["b", "c", "a"]);

  const empty = moveCardLocal(board(), "a", "done", "");
  expect(ids(empty, "done")).toEqual(["a"]);

  expect(moveCardLocal(board(), "missing", "todo", "")).toEqual(board());
  // An anchor that is not in the column moves nothing.
  expect(moveCardLocal(board(), "a", "done", "b")).toEqual(board());
});

test("moveColumnLocal reorders columns, null putting one first", () => {
  const first = moveColumnLocal(board(), "done", null);
  expect(sortedColumns(first).map((c) => c.id)).toEqual(["done", "todo", "doing"]);
  const last = moveColumnLocal(board(), "todo", "done");
  expect(sortedColumns(last).map((c) => c.id)).toEqual(["doing", "done", "todo"]);
});

test("placeAfter lands after equal keys, as the server's `position > prev` did", () => {
  // Two concurrent appends can share a key; the next one sorting after it is the bound.
  const list = [
    { id: "a", position: "1i" },
    { id: "b", position: "1i" },
    { id: "c", position: "2i" },
  ];
  const place = placeAfter(list, "x", "a")!;
  expect(place.position > "1i" && place.position < "2i").toBe(true);
  expect(place.renumber).toEqual([]);
  expect(placeAfter(list, "x", "zz")).toBeNull();
});

test("placeAfter renumbers the list when the key would pass 48 characters", () => {
  const long = "1" + "0".repeat(47) + "1";
  const list = [
    { id: "a", position: "1" },
    { id: "b", position: long },
    { id: "c", position: "2" },
  ];
  // Between "1" and "100…01" there is only room for a 49-character key.
  const place = placeAfter(list, "x", "a")!;
  expect(place.position).toBe("12i");
  expect(place.renumber).toEqual([
    { id: "a", position: "11i" },
    { id: "b", position: "13i" },
    { id: "c", position: "14i" },
  ]);
});

test("a record upsert adds a card and replaces an older one", () => {
  const added = applyRecordsEvent(records(), {
    type: "record.upsert",
    record: cardRec("e", "done", "1i"),
  });
  expect(boardOf(added).cards.map((c) => c.id)).toContain("e");

  const edited = applyRecordsEvent(records(), {
    type: "record.upsert",
    record: cardRec("a", "todo", "1i", 2, { title: "renamed" }),
  });
  expect(boardOf(edited).cards.find((c) => c.id === "a")?.title).toBe("renamed");
});

test("a record upsert with a lower rev than the cached record is ignored", () => {
  const map = applyRecordsEvent(records(), {
    type: "record.upsert",
    record: cardRec("a", "todo", "1i", 5, { title: "newer" }),
  });
  const next = applyRecordsEvent(map, {
    type: "record.upsert",
    record: cardRec("a", "todo", "1i", 4, { title: "older" }),
  });
  expect(next).toBe(map);
  // an equal rev is the same write arriving twice and may replace
  const same = applyRecordsEvent(map, {
    type: "record.upsert",
    record: cardRec("a", "todo", "1i", 5, { title: "newer" }),
  });
  expect(boardOf(same).cards[0]?.title).toBe("newer");
});

test("a record delete removes the card and leaves the board alone when it is unknown", () => {
  const map = records();
  const del = (id: string, collection: string) =>
    applyRecordsEvent(map, { type: "record.delete", id, collection });
  expect(boardOf(del("a", "cards")).cards.map((c) => c.id)).toEqual(["b", "c", "d"]);
  expect(del("nope", "cards")).toBe(map);
});

test("a column upsert replaces or appends", () => {
  const renamed = boardOf(
    applyRecordsEvent(records(), {
      type: "record.upsert",
      record: { ...col("todo", "1i"), rev: 2, data: { title: "Backlog", position: "1i" } },
    }),
  );
  expect(renamed.columns.find((c) => c.id === "todo")?.title).toBe("Backlog");
  expect(renamed.columns).toHaveLength(3);
  const added = boardOf(
    applyRecordsEvent(records(), { type: "record.upsert", record: col("new", "4i") }),
  );
  expect(added.columns).toHaveLength(4);
});

test("labels are kept by name; a deleted label disappears from the cards that carried it", () => {
  const upserted = boardOf(
    applyRecordsEvent(records(), {
      type: "record.upsert",
      record: label("l3", "Alpha", "#22c55e"),
    }),
  );
  expect(upserted.labels.map((l) => l.id)).toEqual(["l3", "l1", "l2"]);

  const tagged = applyRecordsEvent(records(), {
    type: "record.upsert",
    record: cardRec("a", "todo", "1i", 2, { label_ids: ["l1", "l2"] }),
  });
  const deleted = boardOf(
    applyRecordsEvent(tagged, { type: "record.delete", id: "l1", collection: "labels" }),
  );
  expect(deleted.labels.map((l) => l.id)).toEqual(["l2"]);
  expect(deleted.cards[0]?.label_ids).toEqual(["l2"]);
});

test("a card whose column is gone is ignored, and malformed data reads safely", () => {
  const map = applyRecordsEvent(records(), {
    type: "record.delete",
    id: "doing",
    collection: "columns",
  });
  expect(boardOf(map).cards.map((c) => c.id)).toEqual(["a", "b", "c"]);
  const odd = boardOf(
    fromList([
      record("k", "columns", { title: 3, position: "1", wip_limit: "x" }),
      record("z", "cards", { column_id: "k", title: null, assignees: "u", due_at: "" }),
    ]),
  );
  expect(odd.columns[0]).toMatchObject({ title: "", wip_limit: null });
  expect(odd.cards[0]).toMatchObject({ title: "", assignees: [], due_at: null, link_ids: [] });
});

test("isOverLimit warns only past the limit", () => {
  expect(isOverLimit({ wip_limit: null }, 99)).toBe(false);
  expect(isOverLimit({ wip_limit: 3 }, 3)).toBe(false);
  expect(isOverLimit({ wip_limit: 3 }, 4)).toBe(true);
});

test("due dates round-trip as UTC days and are overdue from the next local day", () => {
  expect(dueFromInput("2026-03-04")).toBe("2026-03-04T00:00:00Z");
  expect(dueFromInput("")).toBeNull();
  // A date input accepts a five-digit year, which is no RFC 3339 timestamp.
  expect(dueFromInput("61105-02-20")).toBeUndefined();
  expect(dueFromInput("2026-02-30")).toBeUndefined();
  expect(dueToInput("2026-03-04T00:00:00Z")).toBe("2026-03-04");
  const now = new Date(2026, 2, 4, 15, 0, 0);
  expect(isOverdue("2026-03-04T00:00:00Z", now)).toBe(false);
  expect(isOverdue("2026-03-03T00:00:00Z", now)).toBe(true);
});

test("the card filter combines its parts and treats an empty filter as no filter", () => {
  const now = new Date(2026, 5, 10, 15, 0); // a local afternoon, 10 June
  const base = card("c1", "todo", "1");
  const rich: Card = {
    ...base,
    title: "Fix upload retry",
    description_md: "flaky Networks",
    assignees: ["ana"],
    label_ids: ["bug", "ops"],
    due_at: "2026-06-12T00:00:00Z",
  };

  expect(isFiltering(NO_FILTER)).toBe(false);
  expect(isFiltering({ ...NO_FILTER, query: "  " })).toBe(false);
  expect(matchesFilter(base, NO_FILTER, now)).toBe(true);

  // Text matches the title or the description, whatever the case.
  expect(matchesFilter(rich, { ...NO_FILTER, query: "UPLOAD" }, now)).toBe(true);
  expect(matchesFilter(rich, { ...NO_FILTER, query: "networks" }, now)).toBe(true);
  expect(matchesFilter(rich, { ...NO_FILTER, query: "billing" }, now)).toBe(false);

  // One person, or nobody.
  expect(matchesFilter(rich, { ...NO_FILTER, assignee: "ana" }, now)).toBe(true);
  expect(matchesFilter(rich, { ...NO_FILTER, assignee: "bo" }, now)).toBe(false);
  expect(matchesFilter(rich, { ...NO_FILTER, assignee: UNASSIGNED }, now)).toBe(false);
  expect(matchesFilter(base, { ...NO_FILTER, assignee: UNASSIGNED }, now)).toBe(true);

  // Labels: any of the chosen ones.
  expect(matchesFilter(rich, { ...NO_FILTER, labels: ["ops", "design"] }, now)).toBe(true);
  expect(matchesFilter(rich, { ...NO_FILTER, labels: ["design"] }, now)).toBe(false);

  // Every part has to hold at once.
  expect(matchesFilter(rich, { ...NO_FILTER, query: "upload", assignee: "bo" }, now)).toBe(false);
});

test("the due filter reads days in the viewer's calendar and the stored day in UTC", () => {
  const now = new Date(2026, 5, 10, 23, 30);
  const due = (day: string | null): Card => ({
    ...card("c", "todo", "1"),
    due_at: day === null ? null : `${day}T00:00:00Z`,
  });
  const f = (d: "overdue" | "week" | "none") => ({ ...NO_FILTER, due: d });

  // Yesterday is overdue; today is not.
  expect(matchesFilter(due("2026-06-09"), f("overdue"), now)).toBe(true);
  expect(matchesFilter(due("2026-06-10"), f("overdue"), now)).toBe(false);
  expect(matchesFilter(due(null), f("overdue"), now)).toBe(false);

  // The week runs from today through the sixth day after it.
  expect(matchesFilter(due("2026-06-09"), f("week"), now)).toBe(false);
  expect(matchesFilter(due("2026-06-10"), f("week"), now)).toBe(true);
  expect(matchesFilter(due("2026-06-16"), f("week"), now)).toBe(true);
  expect(matchesFilter(due("2026-06-17"), f("week"), now)).toBe(false);
  expect(matchesFilter(due(null), f("week"), now)).toBe(false);

  expect(matchesFilter(due(null), f("none"), now)).toBe(true);
  expect(matchesFilter(due("2026-06-10"), f("none"), now)).toBe(false);
});
