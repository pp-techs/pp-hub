/* eslint-disable lingui/no-unlocalized-strings -- wire values, alphabet and Intl options, not copy */
// Pure board logic: applying server events and optimistic edits to a snapshot,
// and turning a drop position into the `{column_id, after_id}` the server takes.
// Nothing here touches React, the cache or the network, so it is unit tested
// (`tests/kanban-board-state.test.ts`).

import type { Board, BoardEvent, Card, CardPatch, Column, Label, Link, Person } from "./api";

// ─── Fractional positions ──────────────────────────────────────────────────

// A port of `Between` in `apps/server/ext/kanban/position.go`. The optimistic
// move needs a key between two neighbours to sort the card where it landed; the
// server's answer replaces it a moment later.
const ALPHABET = "0123456789abcdefghijklmnopqrstuvwxyz";

function digit(s: string, i: number): number {
  return i >= s.length ? 0 : ALPHABET.indexOf(s.charAt(i));
}

/** A position that sorts after `a` and before `b`; empty `a` is the start, empty `b` the end. */
export function between(a: string, b: string): string {
  if (b !== "") {
    let n = 0;
    while (n < b.length && digit(a, n) === digit(b, n)) n++;
    if (n > 0) return b.slice(0, n) + between(a.slice(n), b.slice(n));
  }
  const da = digit(a, 0);
  const db = b === "" ? ALPHABET.length : digit(b, 0);
  if (db - da > 1) return ALPHABET.charAt(Math.floor((da + db + 1) / 2));
  if (b.length > 1) return b.slice(0, 1);
  return ALPHABET.charAt(da) + between(a.slice(1), "");
}

/** Order by position, then id, as the server's `ORDER BY position, id` does. */
function byPosition(a: { position: string; id: string }, b: { position: string; id: string }) {
  if (a.position !== b.position) return a.position < b.position ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

export function sortedColumns(board: Board): Column[] {
  return [...board.columns].sort(byPosition);
}

export function cardsIn(board: Board, columnId: string): Card[] {
  return board.cards.filter((c) => c.column_id === columnId).sort(byPosition);
}

// ─── Server events ─────────────────────────────────────────────────────────

function upsertBy<T extends { id: string }>(list: T[], item: T): T[] {
  const i = list.findIndex((x) => x.id === item.id);
  if (i < 0) return [...list, item];
  const next = [...list];
  next[i] = item;
  return next;
}

function sortLabels(labels: Label[]): Label[] {
  return [...labels].sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
}

const sameSet = (a: string[], b: string[]) =>
  a.length === b.length && a.every((x) => b.includes(x));

/** A link known only by id: what a viewer sees until a REST answer resolves it. */
function placeholder(id: string): Link {
  return { id, accessible: false };
}

/**
 * The card an upsert leaves in the cache. A REST answer carries `links`,
 * resolved for this viewer, and wins. An SSE event carries only `link_ids`
 * (one viewer's names must never reach another): the cached resolution is kept
 * when the ids are unchanged, and otherwise replaced by placeholders until the
 * snapshot is refetched (see `linksNeedRefetch`).
 */
function withLinks(cached: Card | undefined, card: Card): Card {
  if (card.links !== undefined) return card;
  if (cached?.links && sameSet(cached.link_ids, card.link_ids)) {
    return { ...card, links: cached.links };
  }
  return { ...card, links: card.link_ids.map(placeholder) };
}

/** Whether `event` left link names unresolved, so the snapshot should be refetched. */
export function linksNeedRefetch(board: Board, event: BoardEvent): boolean {
  if (event.type !== "card.upsert" || event.card.links !== undefined) return false;
  const cached = board.cards.find((c) => c.id === event.card.id);
  if (cached && cached.rev > event.card.rev) return false;
  if (event.card.link_ids.length === 0) return false;
  return !cached?.links || !sameSet(cached.link_ids, event.card.link_ids);
}

/**
 * The snapshot after `event`, or `null` when the event asks for a refetch
 * (`reload`: the server renumbered positions or deleted a column, which no
 * single record can describe). A `card.upsert` older than the cached card is
 * ignored: events and mutation responses race, and the lower `rev` is the stale one.
 */
export function applyEvent(board: Board, event: BoardEvent): Board | null {
  switch (event.type) {
    case "reload":
      return null;
    case "card.upsert": {
      const cached = board.cards.find((c) => c.id === event.card.id);
      if (cached && cached.rev > event.card.rev) return board;
      return { ...board, cards: upsertBy(board.cards, withLinks(cached, event.card)) };
    }
    case "card.delete":
      return board.cards.some((c) => c.id === event.id)
        ? { ...board, cards: board.cards.filter((c) => c.id !== event.id) }
        : board;
    case "column.upsert":
      return { ...board, columns: upsertBy(board.columns, event.column) };
    case "label.upsert":
      return { ...board, labels: sortLabels(upsertBy(board.labels, event.label)) };
    case "label.delete":
      return {
        ...board,
        labels: board.labels.filter((l) => l.id !== event.id),
        cards: board.cards.map((c) =>
          c.label_ids.includes(event.id)
            ? { ...c, label_ids: c.label_ids.filter((id) => id !== event.id) }
            : c,
        ),
      };
    default:
      return null;
  }
}

// ─── Optimistic edits ──────────────────────────────────────────────────────

/** Where `afterId` puts an item in `others` (the list without the moving item). */
function slotAfter(others: { id: string }[], afterId: string | null | undefined): number {
  if (!afterId) return 0;
  const i = others.findIndex((x) => x.id === afterId);
  return i < 0 ? others.length : i + 1;
}

/** Moves a card as the server will: into `columnId`, under `afterId` ("" = top). */
export function moveCardLocal(
  board: Board,
  cardId: string,
  columnId: string,
  afterId: string,
): Board {
  const card = board.cards.find((c) => c.id === cardId);
  if (!card) return board;
  const others = cardsIn(board, columnId).filter((c) => c.id !== cardId);
  const at = slotAfter(others, afterId);
  const position = between(others[at - 1]?.position ?? "", others[at]?.position ?? "");
  return {
    ...board,
    cards: board.cards.map((c) => (c.id === cardId ? { ...c, column_id: columnId, position } : c)),
  };
}

/** Moves a column: `afterId` null puts it first. */
export function moveColumnLocal(board: Board, columnId: string, afterId: string | null): Board {
  if (!board.columns.some((c) => c.id === columnId)) return board;
  const others = sortedColumns(board).filter((c) => c.id !== columnId);
  const at = slotAfter(others, afterId);
  const position = between(others[at - 1]?.position ?? "", others[at]?.position ?? "");
  return {
    ...board,
    columns: board.columns.map((c) => (c.id === columnId ? { ...c, position } : c)),
  };
}

/** A card edit as the cache holds it: assignees are whole people, not ids. */
export type LocalCardPatch = Omit<CardPatch, "assignees" | "links"> & {
  assignees?: Person[];
  links?: Link[];
};

export function patchCardLocal(board: Board, cardId: string, patch: LocalCardPatch): Board {
  return {
    ...board,
    cards: board.cards.map((c) => {
      if (c.id !== cardId) return c;
      const { labels, links, ...rest } = patch;
      return {
        ...c,
        ...rest,
        ...(labels ? { label_ids: labels } : {}),
        ...(links ? { links, link_ids: links.map((l) => l.id) } : {}),
      };
    }),
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

/** What `POST /cards/{id}/move` takes. */
export interface CardDrop {
  column_id: string;
  /** The card to land under; empty puts the card at the top. */
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

/** The `after_id` for a column dragged to `slot` among `ids`; null puts it first. */
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

/** A day a date input holds that the server will take: four-digit year, a real date. */
export function isValidDay(day: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return false;
  const d = new Date(`${day}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === day;
}

/**
 * The RFC 3339 value for an input's day; empty clears the date. A date input
 * lets a person type a five-digit year, which is no timestamp the server takes,
 * so a day that is not valid has no value at all.
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
    // eslint-disable-next-line lingui/no-unlocalized-strings -- Intl option value
    timeZone: "UTC",
  }).format(new Date(dueAt));
}

// ─── Filtering ─────────────────────────────────────────────────────────────
// The toolbar narrows what the board shows; it never changes the board. A
// filter is a view of the snapshot the browser already holds.

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
  if (q && !card.title.toLowerCase().includes(q) && !card.description.toLowerCase().includes(q)) {
    return false;
  }
  if (f.assignee === UNASSIGNED) {
    if (card.assignees.length > 0) return false;
  } else if (f.assignee !== null && !card.assignees.some((p) => p.id === f.assignee)) {
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
