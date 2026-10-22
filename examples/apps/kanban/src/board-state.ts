/* eslint-disable lingui/no-unlocalized-strings -- collection names, wire values and Intl options, not copy */
// Pure board logic: reading a board out of the node's records, placing a moved
// card or column (as the old kanban server did), turning a drop position into a
// move, and the toolbar filter. No React or bridge, so it is unit tested.

import type { AppRecord } from "@pp-hub/app-sdk";
import { inCollection, type RecordMap } from "./bridge/records";
import { between, MAX_POSITION_LEN, spread } from "./position";

// ─── Records ───────────────────────────────────────────────────────────────

/** Collection names; the record shapes are fixed by the migration that wrote existing boards. */
export const COLUMNS = "columns";
export const LABELS = "labels";
export const CARDS = "cards";

export interface Column {
  id: string;
  rev: number;
  title: string;
  /** Fractional index: columns sort by it, bytewise. */
  position: string;
  wip_limit: number | null;
}

export interface Label {
  id: string;
  rev: number;
  name: string;
  /** `#rrggbb`. */
  color: string;
}

/** The data of a `cards` record. */
export interface CardData {
  column_id: string;
  title: string;
  description_md: string;
  /** Fractional index inside the column. */
  position: string;
  /** RFC 3339 UTC midnight of the due day, or null. */
  due_at: string | null;
  /** User ids. */
  assignees: string[];
  label_ids: string[];
  /** Node ids. */
  link_ids: string[];
}

export interface Card extends CardData {
  id: string;
  /** The record's rev; a card edit sends it as `ifRev`. */
  rev: number;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface Board {
  /** In creation order; `sortedColumns` orders them. */
  columns: Column[];
  /** By name. */
  labels: Label[];
  cards: Card[];
}

const str = (v: unknown, fallback = "") => (typeof v === "string" ? v : fallback);
const ids = (v: unknown) =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];

function toColumn(r: AppRecord): Column {
  const limit = r.data.wip_limit;
  return {
    id: r.id,
    rev: r.rev,
    title: str(r.data.title),
    position: str(r.data.position),
    wip_limit: typeof limit === "number" && limit >= 1 ? limit : null,
  };
}

function toLabel(r: AppRecord): Label {
  return { id: r.id, rev: r.rev, name: str(r.data.name), color: str(r.data.color, "#6b7280") };
}

function toCard(r: AppRecord, labels: Set<string>): Card {
  const due = r.data.due_at;
  return {
    id: r.id,
    rev: r.rev,
    column_id: str(r.data.column_id),
    title: str(r.data.title),
    description_md: str(r.data.description_md),
    position: str(r.data.position),
    due_at: typeof due === "string" && due !== "" ? due : null,
    assignees: ids(r.data.assignees),
    // A label deleted since is ignored; nothing cascades on the host.
    label_ids: ids(r.data.label_ids).filter((id) => labels.has(id)),
    link_ids: ids(r.data.link_ids),
    created_by: r.created_by,
    created_at: r.created_at,
    updated_at: r.updated_at,
  };
}

/** The board the records describe. A card whose column is gone is left out, as is a dangling label id. */
export function boardOf(records: RecordMap): Board {
  const columns = inCollection(records, COLUMNS).map(toColumn);
  const labels = inCollection(records, LABELS)
    .map(toLabel)
    .sort((a, b) => a.name.localeCompare(b.name) || cmp(a.id, b.id));
  const columnIds = new Set(columns.map((c) => c.id));
  const labelIds = new Set(labels.map((l) => l.id));
  const cards = inCollection(records, CARDS)
    .map((r) => toCard(r, labelIds))
    .filter((c) => columnIds.has(c.column_id));
  return { columns, labels, cards };
}

/** The data a card's record holds. */
export function cardData(card: Card): CardData {
  const { column_id, title, description_md, position, due_at, assignees, label_ids, link_ids } =
    card;
  return { column_id, title, description_md, position, due_at, assignees, label_ids, link_ids };
}

// ─── Order ─────────────────────────────────────────────────────────────────

const cmp = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

/** Order by position, then id, as the server's `ORDER BY position, id` did (bytewise: positions are ASCII). */
function byPosition(a: { position: string; id: string }, b: { position: string; id: string }) {
  return cmp(a.position, b.position) || cmp(a.id, b.id);
}

export function sortedColumns(board: Board): Column[] {
  return [...board.columns].sort(byPosition);
}

export function cardsIn(board: Board, columnId: string): Card[] {
  return board.cards.filter((c) => c.column_id === columnId).sort(byPosition);
}

// ─── Placing ───────────────────────────────────────────────────────────────

/** Where a moved item goes: its new position, and the others a renumbering rewrites. */
export interface Placement {
  position: string;
  /** Other items of the list whose position changes, when the key would have grown too long. */
  renumber: { id: string; position: string }[];
}

/**
 * The server's placement of an item moving after `afterId` ("" or null = first)
 * in `list` (the destination, in order, which may hold the moving item): between
 * the anchor and the first other item whose position sorts after the anchor's.
 * A key longer than MAX_POSITION_LEN renumbers the list with `spread`. Null
 * when the anchor is not in the list.
 */
export function placeAfter(
  list: { id: string; position: string }[],
  movingId: string,
  afterId: string | null,
): Placement | null {
  const others = list.filter((x) => x.id !== movingId);
  let prev = "";
  if (afterId) {
    const anchor = others.find((x) => x.id === afterId);
    if (!anchor) return null;
    prev = anchor.position;
  }
  const next = others.find((x) => x.position > prev)?.position ?? "";
  const position = between(prev, next);
  if (position.length <= MAX_POSITION_LEN) return { position, renumber: [] };

  const at = afterId ? others.findIndex((x) => x.id === afterId) + 1 : 0;
  const order = [
    ...others.slice(0, at).map((x) => x.id),
    movingId,
    ...others.slice(at).map((x) => x.id),
  ];
  const keys = spread(order.length);
  const current = new Map(others.map((x) => [x.id, x.position]));
  return {
    position: keys[at]!,
    renumber: order.flatMap((id, i) =>
      id !== movingId && current.get(id) !== keys[i] ? [{ id, position: keys[i]! }] : [],
    ),
  };
}

/** Where a card moving into `columnId` under `afterId` ("" = top) lands. */
export function placeCard(
  board: Board,
  cardId: string,
  columnId: string,
  afterId: string,
): Placement | null {
  return placeAfter(cardsIn(board, columnId), cardId, afterId);
}

/** Where a column moving after `afterId` (null = first) lands. */
export function placeColumn(
  board: Board,
  columnId: string,
  afterId: string | null,
): Placement | null {
  return placeAfter(sortedColumns(board), columnId, afterId);
}

/** Moves a card as the move will: into `columnId`, under `afterId` ("" = top). */
export function moveCardLocal(
  board: Board,
  cardId: string,
  columnId: string,
  afterId: string,
): Board {
  const place = board.cards.some((c) => c.id === cardId)
    ? placeCard(board, cardId, columnId, afterId)
    : null;
  if (!place) return board;
  const renumbered = new Map(place.renumber.map((r) => [r.id, r.position]));
  return {
    ...board,
    cards: board.cards.map((c) =>
      c.id === cardId
        ? { ...c, column_id: columnId, position: place.position }
        : renumbered.has(c.id)
          ? { ...c, position: renumbered.get(c.id)! }
          : c,
    ),
  };
}

/** Moves a column: `afterId` null puts it first. */
export function moveColumnLocal(board: Board, columnId: string, afterId: string | null): Board {
  const place = board.columns.some((c) => c.id === columnId)
    ? placeColumn(board, columnId, afterId)
    : null;
  if (!place) return board;
  const renumbered = new Map(place.renumber.map((r) => [r.id, r.position]));
  return {
    ...board,
    columns: board.columns.map((c) =>
      c.id === columnId
        ? { ...c, position: place.position }
        : renumbered.has(c.id)
          ? { ...c, position: renumbered.get(c.id)! }
          : c,
    ),
  };
}

// ─── Drop positions ────────────────────────────────────────────────────────

/**
 * The insertion slot for a pointer: how many of the items, given by the
 * midpoints of their boxes along the drag axis, lie before it. Slot `n` is the end.
 */
export function slotFromPointer(midpoints: number[], pointer: number): number {
  let slot = 0;
  for (const mid of midpoints) {
    if (pointer > mid) slot += 1;
  }
  return slot;
}

/** A card move: the column, and the card to land under ("" = the top). */
export interface CardDrop {
  column_id: string;
  after_id: string;
}

/**
 * Turns a drop slot into a move. `ids` are the cards of the target column as
 * displayed (the moving card included when it is a move within the column) and
 * `slot` is the gap before `ids[slot]` — `ids.length` is the bottom. The moving
 * card leaves its own place first, so the gap is counted without it.
 */
export function dropPosition(
  columnId: string,
  ids: string[],
  slot: number,
  movingId: string,
): CardDrop {
  const rest = ids.filter((id) => id !== movingId);
  const at = slot - ids.slice(0, slot).filter((id) => id === movingId).length;
  return { column_id: columnId, after_id: at <= 0 ? "" : (rest[at - 1] ?? "") };
}

/** Whether dropping `cardId` at `drop` would leave it where it is. */
export function isNoopCardMove(board: Board, cardId: string, drop: CardDrop): boolean {
  const card = board.cards.find((c) => c.id === cardId);
  if (!card || card.column_id !== drop.column_id) return false;
  const ids = cardsIn(board, drop.column_id).map((c) => c.id);
  return (ids[ids.indexOf(cardId) - 1] ?? "") === drop.after_id;
}

/** The column to land after for a column dragged to `slot` among `ids`; null puts it first. */
export function columnDropAfter(ids: string[], slot: number, movingId: string): string | null {
  const rest = ids.filter((id) => id !== movingId);
  const at = slot - ids.slice(0, slot).filter((id) => id === movingId).length;
  return at <= 0 ? null : (rest[at - 1] ?? null);
}

/** Whether dropping a column at `afterId` would leave it where it is. */
export function isNoopColumnMove(board: Board, columnId: string, afterId: string | null): boolean {
  const ids = sortedColumns(board).map((c) => c.id);
  return (ids[ids.indexOf(columnId) - 1] ?? null) === afterId;
}

// ─── Columns ───────────────────────────────────────────────────────────────

/** A column holds more cards than its WIP limit allows. */
export function isOverLimit(column: Pick<Column, "wip_limit">, count: number): boolean {
  return column.wip_limit !== null && count > column.wip_limit;
}

// ─── Due dates ─────────────────────────────────────────────────────────────
// A due date is a calendar day with no time of day. It is stored as UTC
// midnight and always read back in UTC, so it never slides a day with the
// viewer's time zone.

/** `YYYY-MM-DD` for a `<input type="date">`. */
export function dueToInput(dueAt: string): string {
  return new Date(dueAt).toISOString().slice(0, 10);
}

/** A day a date input holds that makes a real timestamp: four-digit year, a real date. */
export function isValidDay(day: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return false;
  const d = new Date(`${day}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === day;
}

/**
 * The RFC 3339 value for an input's day; empty clears the date. A date input
 * lets a person type a five-digit year, which is no RFC 3339 timestamp, so a
 * day that is not valid has no value at all.
 */
export function dueFromInput(day: string): string | null | undefined {
  if (day === "") return null;
  return isValidDay(day) ? `${day}T00:00:00Z` : undefined;
}

function localDay(now: Date): string {
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const date = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${date}`;
}

/** The due day is before today (in the viewer's calendar). */
export function isOverdue(dueAt: string, now: Date = new Date()): boolean {
  return dueToInput(dueAt) < localDay(now);
}

export function formatDue(dueAt: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(new Date(dueAt));
}

// ─── Filtering ─────────────────────────────────────────────────────────────
// The toolbar narrows what the board shows; it never changes the board.

/** The `assignee` filter value for cards nobody is assigned to. */
export const UNASSIGNED = "unassigned";

export type DueFilter = "overdue" | "week" | "none";

export interface CardFilter {
  /** Text the title or description contains, ignoring case. */
  query: string;
  /** A person's id, `UNASSIGNED`, or null for anyone. */
  assignee: string | null;
  /** Cards that carry at least one of these labels; empty is any. */
  labels: string[];
  /** `week` is today through the next six days; null is any. */
  due: DueFilter | null;
}

export const NO_FILTER: CardFilter = { query: "", assignee: null, labels: [], due: null };

export function isFiltering(f: CardFilter): boolean {
  return f.query.trim() !== "" || f.assignee !== null || f.labels.length > 0 || f.due !== null;
}

export function matchesFilter(card: Card, f: CardFilter, now: Date = new Date()): boolean {
  const q = f.query.trim().toLowerCase();
  if (
    q &&
    !card.title.toLowerCase().includes(q) &&
    !card.description_md.toLowerCase().includes(q)
  ) {
    return false;
  }
  if (f.assignee === UNASSIGNED) {
    if (card.assignees.length > 0) return false;
  } else if (f.assignee !== null && !card.assignees.includes(f.assignee)) {
    return false;
  }
  if (f.labels.length > 0 && !card.label_ids.some((id) => f.labels.includes(id))) return false;
  switch (f.due) {
    case "none":
      return card.due_at === null;
    case "overdue":
      return card.due_at !== null && isOverdue(card.due_at, now);
    case "week": {
      if (card.due_at === null) return false;
      const day = dueToInput(card.due_at);
      const end = new Date(now);
      end.setDate(end.getDate() + 6);
      return day >= localDay(now) && day <= localDay(end);
    }
    default:
      return true;
  }
}

/** The palette a label's colour is chosen from. */
export const LABEL_COLORS = [
  "#ef4444",
  "#f97316",
  "#eab308",
  "#22c55e",
  "#14b8a6",
  "#3b82f6",
  "#8b5cf6",
  "#ec4899",
  "#6b7280",
] as const;
