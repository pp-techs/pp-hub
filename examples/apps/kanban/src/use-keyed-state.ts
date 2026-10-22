import { useState } from "react";

/**
 * State that resets itself when the thing it describes changes.
 *
 * This exists because better-modal keeps a closed modal **mounted**: `resolve`
 * only flips `isOpen` to false, the entry stays in the store, and
 * `ModalContainer` goes on rendering the component. A plain `useState`
 * therefore initialises once for the life of the app, so reopening a modal for
 * a different item leaves it holding the previous item's state — the preview
 * modal reopened on the last file viewed, and the move dialog kept the folder
 * chosen last time, which could have moved files somewhere never selected.
 *
 * `key` identifies what the state belongs to. When it changes, the value snaps
 * back to `initial` during render — React's documented alternative to a
 * resetting effect, so the correct value is there on the first paint rather
 * than after a flash of the wrong one.
 */
export function useKeyedState<K, V>(key: K, initial: V): [V, (next: V | ((prev: V) => V)) => void] {
  const [held, setHeld] = useState<{ key: K; value: V }>({ key, value: initial });

  // Correctness comes from the read-through below and from `set` stamping the
  // current key; this adjustment keeps the stored key from going stale in the
  // meantime and releases the previous subject's value. It is React's
  // documented alternative to a resetting effect.
  if (held.key !== key) {
    setHeld({ key, value: initial });
  }

  // The render that discovers the change reads through to `initial`: the
  // re-render carrying the new state has not happened yet, and showing the
  // previous subject's value for one frame is the whole bug.
  const value = held.key === key ? held.value : initial;

  const set = (next: V | ((prev: V) => V)) =>
    setHeld((prev) => {
      const base = prev.key === key ? prev.value : initial;
      return {
        key,
        value: typeof next === "function" ? (next as (prev: V) => V)(base) : next,
      };
    });

  return [value, set];
}
