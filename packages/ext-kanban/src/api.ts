/* eslint-disable lingui/no-unlocalized-strings -- URL paths and wire values, not copy */
// Typed calls to the kanban extension's routes, `/api/v1/ext/kanban/boards/{id}`.
// The wire types mirror `apps/server/ext/kanban/handlers.go`.

import { api, ApiError } from "@pp-hub/ext-sdk";
import type { Caps, Person } from "@pp-hub/ext-sdk";

export type { Person };

export interface Column {
  id: string;
  title: string;
  /** Fractional index: columns sort by it, bytewise. */
  position: string;
  wip_limit: number | null;
}

export interface Label {
  id: string;
  name: string;
  /** `#rrggbb`. */
  color: string;
}

/** A node a card points at, as one viewer may see it. */
export interface Link {
  id: string;
  /** False when the viewer cannot open the node; then no name or kind is given. */
  accessible: boolean;
  name?: string;
  kind?: "dir" | "file" | "ext";
  ext_kind?: string;
  space_id?: string;
}

export interface Card {
  id: string;
  column_id: string;
  title: string;
  description: string;
  /** Fractional index inside the column. */
  position: string;
  /** RFC 3339, or null. */
  due_at: string | null;
  /** Bumped by the server on every write; `PATCH` must echo it as `if_rev`. */
  rev: number;
  assignees: Person[];
  label_ids: string[];
  comment_count: number;
  /** The nodes the card links to; always present. */
  link_ids: string[];
  /**
   * `link_ids` resolved for the viewer. Only REST answers carry it: an SSE
   * event goes to everyone, so it has the ids and no names.
   */
  links?: Link[];
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface Board {
  id: string;
  name: string;
  caps: Caps;
  columns: Column[];
  cards: Card[];
  labels: Label[];
}

/** One message of the `board` SSE stream. */
export type BoardEvent =
  | { type: "card.upsert"; card: Card }
  | { type: "card.delete"; id: string }
  | { type: "column.upsert"; column: Column }
  | { type: "label.upsert"; label: Label }
  | { type: "label.delete"; id: string }
  | { type: "reload" };

/** Fields of a card `PATCH`; an absent field is left as it is. */
export interface CardPatch {
  title?: string;
  description?: string;
  /** RFC 3339 sets, null clears. */
  due_at?: string | null;
  /** The complete assignee set, by user id. */
  assignees?: string[];
  /** The complete label set. */
  labels?: string[];
  /** The complete set of linked node ids (at most 20). */
  links?: string[];
}

export interface ColumnPatch {
  title?: string;
  /** A number sets, null clears. */
  wip_limit?: number | null;
  /** Moves the column: absent = no move, null = first, an id = after that column. */
  after_id?: string | null;
}

const base = (nodeId: string) => `api/v1/ext/kanban/boards/${nodeId}`;

/** The URL of the board's event stream. */
export const eventsUrl = (nodeId: string) => `/${base(nodeId)}/events`;

async function ok(res: Response): Promise<Response> {
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res;
}

export async function getBoard(nodeId: string): Promise<Board> {
  const res = await ok(await api.get(`${base(nodeId)}/`));
  return res.json() as Promise<Board>;
}

export async function createColumn(
  nodeId: string,
  body: { title: string; wip_limit?: number | null },
): Promise<Column> {
  const res = await ok(await api.post(`${base(nodeId)}/columns`, { json: body }));
  return res.json() as Promise<Column>;
}

export async function updateColumn(
  nodeId: string,
  columnId: string,
  patch: ColumnPatch,
): Promise<Column> {
  const res = await ok(await api.patch(`${base(nodeId)}/columns/${columnId}`, { json: patch }));
  return res.json() as Promise<Column>;
}

/** `moveTo` is required by the server when the column holds cards. */
export async function deleteColumn(
  nodeId: string,
  columnId: string,
  moveTo?: string,
): Promise<void> {
  await ok(
    await api.delete(`${base(nodeId)}/columns/${columnId}`, {
      searchParams: moveTo ? { move_to: moveTo } : undefined,
    }),
  );
}

export async function createCard(
  nodeId: string,
  body: { column_id: string; title: string },
): Promise<Card> {
  const res = await ok(await api.post(`${base(nodeId)}/cards`, { json: body }));
  return res.json() as Promise<Card>;
}

export async function updateCard(
  nodeId: string,
  cardId: string,
  ifRev: number,
  patch: CardPatch,
): Promise<Card> {
  const res = await ok(
    await api.patch(`${base(nodeId)}/cards/${cardId}`, { json: { ...patch, if_rev: ifRev } }),
  );
  return res.json() as Promise<Card>;
}

/** `afterId` is the card to land under; empty puts the card at the top. */
export async function moveCard(
  nodeId: string,
  cardId: string,
  columnId: string,
  afterId: string,
): Promise<Card> {
  const res = await ok(
    await api.post(`${base(nodeId)}/cards/${cardId}/move`, {
      json: { column_id: columnId, after_id: afterId },
    }),
  );
  return res.json() as Promise<Card>;
}

export async function deleteCard(nodeId: string, cardId: string): Promise<void> {
  await ok(await api.delete(`${base(nodeId)}/cards/${cardId}`));
}

export async function createLabel(
  nodeId: string,
  body: { name: string; color: string },
): Promise<Label> {
  const res = await ok(await api.post(`${base(nodeId)}/labels`, { json: body }));
  return res.json() as Promise<Label>;
}

export async function updateLabel(
  nodeId: string,
  labelId: string,
  patch: { name?: string; color?: string },
): Promise<Label> {
  const res = await ok(await api.patch(`${base(nodeId)}/labels/${labelId}`, { json: patch }));
  return res.json() as Promise<Label>;
}

export async function deleteLabel(nodeId: string, labelId: string): Promise<void> {
  await ok(await api.delete(`${base(nodeId)}/labels/${labelId}`));
}

/** People who can open the board, for the assignee picker. */
export async function searchPeople(nodeId: string, q: string): Promise<Person[]> {
  const res = await ok(await api.get(`${base(nodeId)}/people`, { searchParams: { q } }));
  return res.json() as Promise<Person[]>;
}
