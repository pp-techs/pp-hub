// Pure checklist logic: items read from the node's records, and the rules the
// old checklist server enforced. No React or bridge, so it is unit tested.

import type { AppRecord } from "@pp-hub/app-sdk";
import { inCollection, type RecordMap } from "./bridge/records";

/** The collection that holds the items: `{ text, done }`, in creation order. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- collection name
export const ITEMS = "items";
/** The most items a checklist holds. */
export const MAX_ITEMS = 500;
/** The longest item text, in characters. */
export const MAX_TEXT = 500;

export interface Item {
  id: string;
  rev: number;
  text: string;
  done: boolean;
}

export function toItem(record: AppRecord): Item {
  const { text, done } = record.data;
  return {
    id: record.id,
    rev: record.rev,
    text: typeof text === "string" ? text : "",
    done: done === true,
  };
}

/** The items in creation order, which is the order they are listed in. */
export function itemsOf(records: RecordMap): Item[] {
  return inCollection(records, ITEMS).map(toItem);
}

export function progress(items: Item[]): { done: number; total: number } {
  return { done: items.filter((i) => i.done).length, total: items.length };
}

/** An item's text as stored: trimmed, 1 to 500 characters; null when it is not one. */
export function cleanText(text: string): string | null {
  const trimmed = text.trim();
  // Characters (code points), as the server counted runes.
  const length = Array.from(trimmed).length;
  return length === 0 || length > MAX_TEXT ? null : trimmed;
}
