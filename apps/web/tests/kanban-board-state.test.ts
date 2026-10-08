import { expect, test } from "vite-plus/test";
import type { Board, Card, Column } from "../../../packages/ext-kanban/src/api.ts";
import {
  applyEvent,
  between,
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
  linksNeedRefetch,
  matchesFilter,
  moveCardLocal,
  moveColumnLocal,
  NO_FILTER,
  patchCardLocal,
  slotFromPointer,
  sortedColumns,
  UNASSIGNED,
} from "../../../packages/ext-kanban/src/board-state.ts";

const col = (id: string, position: string, wip_limit: number | null = null): Column => ({
  id,
  title: id,
  position,
  wip_limit,
});

const card = (id: string, column_id: string, position: string, rev = 1): Card => ({
  id,
  column_id,
  title: id,
  description: "",
  position,
  due_at: null,
  rev,
  assignees: [],
  label_ids: [],
  comment_count: 0,
  link_ids: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
});

const board = (): Board => ({
  id: "b",
  name: "Board",
  caps: { view: true, download: true, share: true, edit: true, owner: true },
  columns: [col("todo", "1i"), col("doing", "2i"), col("done", "3i")],
  cards: [
    card("a", "todo", "1i"),
    card("b", "todo", "2i"),
    card("c", "todo", "3i"),
    card("d", "doing", "1i"),
  ],
  labels: [
    { id: "l1", name: "Bug", color: "#ef4444" },
    { id: "l2", name: "Idea", color: "#3b82f6" },
  ],
});

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
});

test("moveColumnLocal reorders columns, null putting one first", () => {
  const first = moveColumnLocal(board(), "done", null);
  expect(sortedColumns(first).map((c) => c.id)).toEqual(["done", "todo", "doing"]);
  const last = moveColumnLocal(board(), "todo", "done");
  expect(sortedColumns(last).map((c) => c.id)).toEqual(["doing", "done", "todo"]);
});

test("card.upsert adds a card and replaces an older one", () => {
  const added = applyEvent(board(), { type: "card.upsert", card: card("e", "done", "1i") });
  expect(added?.cards.map((c) => c.id)).toContain("e");

  const edited = applyEvent(board(), {
    type: "card.upsert",
    card: { ...card("a", "todo", "1i", 2), title: "renamed" },
  });
  expect(edited?.cards.find((c) => c.id === "a")?.title).toBe("renamed");
});

test("card.upsert with a lower rev than the cached card is ignored", () => {
  const b = board();
  b.cards[0] = { ...b.cards[0]!, rev: 5, title: "newer" };
  const next = applyEvent(b, {
    type: "card.upsert",
    card: { ...card("a", "todo", "1i", 4), title: "older" },
  });
  expect(next).toBe(b);
  // an equal rev is the same write arriving twice and may replace
  const same = applyEvent(b, {
    type: "card.upsert",
    card: { ...card("a", "todo", "1i", 5), title: "newer" },
  });
  expect(same?.cards[0]?.title).toBe("newer");
});

test("card.delete removes the card and leaves the board alone when it is unknown", () => {
  const b = board();
  expect(applyEvent(b, { type: "card.delete", id: "a" })?.cards.map((c) => c.id)).toEqual([
    "b",
    "c",
    "d",
  ]);
  expect(applyEvent(b, { type: "card.delete", id: "nope" })).toBe(b);
});

test("column.upsert replaces or appends", () => {
  const renamed = applyEvent(board(), {
    type: "column.upsert",
    column: { ...col("todo", "1i"), title: "Backlog" },
  });
  expect(renamed?.columns.find((c) => c.id === "todo")?.title).toBe("Backlog");
  expect(renamed?.columns).toHaveLength(3);
  const added = applyEvent(board(), { type: "column.upsert", column: col("new", "4i") });
  expect(added?.columns).toHaveLength(4);
});

test("label.upsert keeps labels by name; label.delete also strips it from cards", () => {
  const b = board();
  const upserted = applyEvent(b, {
    type: "label.upsert",
    label: { id: "l3", name: "Alpha", color: "#22c55e" },
  });
  expect(upserted?.labels.map((l) => l.id)).toEqual(["l3", "l1", "l2"]);

  b.cards[0] = { ...b.cards[0]!, label_ids: ["l1", "l2"] };
  const deleted = applyEvent(b, { type: "label.delete", id: "l1" });
  expect(deleted?.labels.map((l) => l.id)).toEqual(["l2"]);
  expect(deleted?.cards[0]?.label_ids).toEqual(["l2"]);
});

test("reload asks for a refetch", () => {
  expect(applyEvent(board(), { type: "reload" })).toBeNull();
});

test("patchCardLocal maps labels to label_ids and takes whole assignees", () => {
  const next = patchCardLocal(board(), "a", {
    title: "T",
    labels: ["l2"],
    assignees: [{ id: "u1", name: "Ann" }],
    due_at: "2026-02-03T00:00:00Z",
  });
  const a = next.cards[0]!;
  expect(a.title).toBe("T");
  expect(a.label_ids).toEqual(["l2"]);
  expect(a.assignees).toEqual([{ id: "u1", name: "Ann" }]);
  expect(a.due_at).toBe("2026-02-03T00:00:00Z");
});

test("isOverLimit warns only past the limit", () => {
  expect(isOverLimit({ wip_limit: null }, 99)).toBe(false);
  expect(isOverLimit({ wip_limit: 3 }, 3)).toBe(false);
  expect(isOverLimit({ wip_limit: 3 }, 4)).toBe(true);
});

test("due dates round-trip as UTC days and are overdue from the next local day", () => {
  expect(dueFromInput("2026-03-04")).toBe("2026-03-04T00:00:00Z");
  expect(dueFromInput("")).toBeNull();
  // A date input accepts a five-digit year; the server would refuse it.
  expect(dueFromInput("61105-02-20")).toBeUndefined();
  expect(dueFromInput("2026-02-30")).toBeUndefined();
  expect(dueToInput("2026-03-04T00:00:00Z")).toBe("2026-03-04");
  const now = new Date(2026, 2, 4, 15, 0, 0);
  expect(isOverdue("2026-03-04T00:00:00Z", now)).toBe(false);
  expect(isOverdue("2026-03-03T00:00:00Z", now)).toBe(true);
});

const resolved = { id: "n1", accessible: true, name: "Plan.md", kind: "file" as const };

test("an SSE card.upsert keeps the cached link names while the ids are unchanged", () => {
  const b = board();
  b.cards[0] = { ...b.cards[0]!, link_ids: ["n1"], links: [resolved] };
  const event = {
    type: "card.upsert" as const,
    card: { ...card("a", "todo", "1i", 2), link_ids: ["n1"], title: "renamed" },
  };
  const next = applyEvent(b, event);
  expect(next?.cards[0]?.title).toBe("renamed");
  expect(next?.cards[0]?.links).toEqual([resolved]);
  expect(linksNeedRefetch(b, event)).toBe(false);
});

test("an SSE card.upsert with other link ids gets placeholders and asks for a refetch", () => {
  const b = board();
  b.cards[0] = { ...b.cards[0]!, link_ids: ["n1"], links: [resolved] };
  const event = {
    type: "card.upsert" as const,
    card: { ...card("a", "todo", "1i", 2), link_ids: ["n1", "n2"] },
  };
  const next = applyEvent(b, event);
  expect(next?.cards[0]?.link_ids).toEqual(["n1", "n2"]);
  expect(next?.cards[0]?.links).toEqual([
    { id: "n1", accessible: false },
    { id: "n2", accessible: false },
  ]);
  expect(linksNeedRefetch(b, event)).toBe(true);
});

test("a new card with links from an event is a placeholder; none means nothing to refetch", () => {
  const b = board();
  const linked = {
    type: "card.upsert" as const,
    card: { ...card("e", "done", "1i"), link_ids: ["n9"] },
  };
  expect(applyEvent(b, linked)?.cards.find((c) => c.id === "e")?.links).toEqual([
    { id: "n9", accessible: false },
  ]);
  expect(linksNeedRefetch(b, linked)).toBe(true);
  const plain = { type: "card.upsert" as const, card: card("f", "done", "2i") };
  expect(linksNeedRefetch(b, plain)).toBe(false);
});

test("a REST card, which carries links, is stored as it came", () => {
  const rest = { ...card("a", "todo", "1i", 2), link_ids: ["n1"], links: [resolved] };
  const event = { type: "card.upsert" as const, card: rest };
  expect(applyEvent(board(), event)?.cards[0]?.links).toEqual([resolved]);
  expect(linksNeedRefetch(board(), event)).toBe(false);
});

test("patchCardLocal applies links to both link_ids and links", () => {
  const next = patchCardLocal(board(), "a", { links: [resolved] });
  expect(next.cards[0]?.link_ids).toEqual(["n1"]);
  expect(next.cards[0]?.links).toEqual([resolved]);
});

test("the card filter combines its parts and treats an empty filter as no filter", () => {
  const now = new Date(2026, 5, 10, 15, 0); // a local afternoon, 10 June
  const ana = { id: "ana", name: "Ana" };
  const base = card("c1", "todo", "1");
  const rich: Card = {
    ...base,
    title: "Fix upload retry",
    description: "flaky Networks",
    assignees: [ana],
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
