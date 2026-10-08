// Pure list logic: applying a server event to a snapshot. No React, cache or
// network, so it is unit tested (`tests/checklist-list-state.test.ts`).

import type { Item, List, ListEvent } from "./api";

/** Items in creation order, which is how the server sends them. */
export function upsertItem(items: Item[], item: Item): Item[] {
  const i = items.findIndex((x) => x.id === item.id);
  if (i < 0) return [...items, item];
  const next = [...items];
  next[i] = item;
  return next;
}

export function applyEvent(list: List, event: ListEvent): List {
  switch (event.type) {
    case "item.upsert":
      return { ...list, items: upsertItem(list.items, event.item) };
    case "item.delete":
      return { ...list, items: list.items.filter((i) => i.id !== event.id) };
  }
}

export function progress(items: Item[]): { done: number; total: number } {
  return { done: items.filter((i) => i.done).length, total: items.length };
}
