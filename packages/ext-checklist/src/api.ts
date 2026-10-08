/* eslint-disable lingui/no-unlocalized-strings -- URL paths and wire values, not copy */
// Typed calls to the checklist extension's routes, `/api/v1/ext/checklist/lists/{id}`.
// The wire types mirror `apps/server/ext/checklist/handlers.go`.

import { api, ApiError } from "@pp-hub/ext-sdk";
import type { Caps } from "@pp-hub/ext-sdk";

export interface Item {
  id: string;
  text: string;
  done: boolean;
  created_at: string;
}

export interface List {
  id: string;
  name: string;
  caps: Caps;
  items: Item[];
}

/** One message of the `list` SSE stream. */
export type ListEvent = { type: "item.upsert"; item: Item } | { type: "item.delete"; id: string };

const base = (nodeId: string) => `api/v1/ext/checklist/lists/${nodeId}`;

/** The URL of the list's event stream. */
export const eventsUrl = (nodeId: string) => `/${base(nodeId)}/events`;

async function ok(res: Response): Promise<Response> {
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res;
}

export async function getList(nodeId: string): Promise<List> {
  return (await ok(await api.get(`${base(nodeId)}/`))).json() as Promise<List>;
}

export async function createItem(nodeId: string, text: string): Promise<Item> {
  return (
    await ok(await api.post(`${base(nodeId)}/items`, { json: { text } }))
  ).json() as Promise<Item>;
}

export async function updateItem(
  nodeId: string,
  itemId: string,
  patch: { text?: string; done?: boolean },
): Promise<Item> {
  return (
    await ok(await api.patch(`${base(nodeId)}/items/${itemId}`, { json: patch }))
  ).json() as Promise<Item>;
}

export async function deleteItem(nodeId: string, itemId: string): Promise<void> {
  await ok(await api.delete(`${base(nodeId)}/items/${itemId}`));
}
