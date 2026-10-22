/* eslint-disable lingui/no-unlocalized-strings -- rule codes; the UI words them (board-context.tsx) */
// The rules the kanban server enforced on every write, now checked by the app
// before it writes a record: lengths, limits, label names and the shape of a
// card. Pure, so it is unit tested; `ops.ts` calls it.

import type { Board, CardData } from "./board-state";

export const LIMITS = {
  columns: 30,
  labels: 50,
  cards: 5000,
  /** Links per card. */
  links: 20,
  /** Assignees or labels per card. */
  ids: 50,
  columnTitle: 200,
  cardTitle: 500,
  description: 20000,
  labelName: 60,
};

/** Why a write was refused before it reached the host; the UI words each one. */
export type Rule =
  | "columns_full"
  | "labels_full"
  | "cards_full"
  | "links_full"
  | "too_many"
  | "label_exists"
  | "label_missing"
  | "link_unavailable"
  | "invalid"
  | "gone";

export class RuleError extends Error {
  readonly rule: Rule;
  constructor(rule: Rule) {
    super(rule);
    this.rule = rule;
  }
}

/** Characters (code points), as the server counted runes. */
const length = (s: string) => Array.from(s).length;

/** `value` trimmed, holding 1 to `max` characters; a RuleError otherwise. */
export function cleanText(value: string, max: number): string {
  const trimmed = value.trim();
  const n = length(trimmed);
  if (n === 0 || n > max) throw new RuleError("invalid");
  return trimmed;
}

export const COLOR = /^#[0-9a-fA-F]{6}$/;

/** A WIP limit: a positive whole number, or null for none. */
export function checkWipLimit(limit: number | null): number | null {
  if (limit !== null && !(Number.isInteger(limit) && limit >= 1)) throw new RuleError("invalid");
  return limit;
}

/** A label's name and colour; `selfId` is the label being renamed. Names are unique per board, case-sensitive. */
export function checkLabel(
  board: Board,
  input: { name?: string; color?: string },
  selfId?: string,
): { name?: string; color?: string } {
  const out: { name?: string; color?: string } = {};
  if (input.color !== undefined) {
    if (!COLOR.test(input.color)) throw new RuleError("invalid");
    out.color = input.color;
  }
  if (input.name !== undefined) {
    out.name = cleanText(input.name, LIMITS.labelName);
    if (board.labels.some((l) => l.name === out.name && l.id !== selfId)) {
      throw new RuleError("label_exists");
    }
  }
  if (selfId === undefined && board.labels.length >= LIMITS.labels) {
    throw new RuleError("labels_full");
  }
  return out;
}

/** Where a new card goes: refused when the column is gone or the board is full. */
export function checkNewCard(board: Board, columnId: string): void {
  if (!board.columns.some((c) => c.id === columnId)) throw new RuleError("gone");
  if (board.cards.length >= LIMITS.cards) throw new RuleError("cards_full");
}

/** A new column: refused when the board holds 30. */
export function checkNewColumn(board: Board): void {
  if (board.columns.length >= LIMITS.columns) throw new RuleError("columns_full");
}

const dedupe = (ids: string[]) => [...new Set(ids)];

/** An RFC 3339 timestamp, as Go's time.Parse(time.RFC3339) takes it. */
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

/**
 * The card a write leaves, checked: title trimmed (1–500), description at
 * most 20 000 characters, due date RFC 3339 or null, id sets without
 * duplicates (≤ 50 assignees and labels, ≤ 20 links), every label one of the
 * board's, no link to the board itself.
 */
export function checkCard(board: Board, data: CardData, boardNodeId: string): CardData {
  const title = cleanText(data.title, LIMITS.cardTitle);
  if (length(data.description_md) > LIMITS.description) throw new RuleError("invalid");
  if (
    data.due_at !== null &&
    !(RFC3339.test(data.due_at) && !Number.isNaN(Date.parse(data.due_at)))
  ) {
    throw new RuleError("invalid");
  }
  const assignees = dedupe(data.assignees);
  const labelIds = dedupe(data.label_ids);
  const linkIds = dedupe(data.link_ids);
  if (assignees.length > LIMITS.ids || labelIds.length > LIMITS.ids) {
    throw new RuleError("too_many");
  }
  if (linkIds.length > LIMITS.links) throw new RuleError("links_full");
  const labels = new Set(board.labels.map((l) => l.id));
  if (labelIds.some((id) => !labels.has(id))) throw new RuleError("label_missing");
  if (linkIds.includes(boardNodeId)) throw new RuleError("link_unavailable");
  return { ...data, title, assignees, label_ids: labelIds, link_ids: linkIds };
}
