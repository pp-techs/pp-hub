import type { Node } from "@/lib/api";

/** Which modifier the click carried. `toggle` wins over `range`. */
export interface ClickModifiers {
  /** ⌘ on macOS, Ctrl elsewhere. */
  toggle: boolean;
  /** Shift. */
  range: boolean;
}

export interface Selection {
  ids: Set<string>;
  /** The node a later shift-click extends from. */
  anchorId: string | null;
}

/**
 * What clicking `nodeId` does to the selection.
 *
 * A plain click replaces the selection, ⌘/Ctrl adds or removes one node, and
 * Shift takes everything between the anchor and the click, inclusive. Both
 * modifiers at once behave as ⌘ alone — toggling is the more specific intent,
 * and a range that also toggles has no obvious meaning.
 *
 * `order` is the flat listing order; a Shift-click with no anchor, or over a
 * node the current listing no longer holds (it was renamed out of a filter,
 * say), falls back to replacing.
 */
export function nextSelection(
  order: Node[],
  current: Selection,
  nodeId: string,
  mods: ClickModifiers,
): Selection {
  if (mods.toggle) {
    const ids = new Set(current.ids);
    if (!ids.delete(nodeId)) ids.add(nodeId);
    return { ids, anchorId: nodeId };
  }

  if (mods.range && current.anchorId !== null) {
    const from = order.findIndex((n) => n.id === current.anchorId);
    const to = order.findIndex((n) => n.id === nodeId);
    if (from !== -1 && to !== -1) {
      const [lo, hi] = from <= to ? [from, to] : [to, from];
      return {
        ids: new Set(order.slice(lo, hi + 1).map((n) => n.id)),
        anchorId: current.anchorId,
      };
    }
  }

  return { ids: new Set([nodeId]), anchorId: nodeId };
}
