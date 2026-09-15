import { useEffect, useState } from "react";

/**
 * Trails `value` by `delay` ms, resetting the timer on every change.
 *
 * Search runs on the settled value while the input stays fully controlled by
 * the typed one, so the field never lags behind the keyboard.
 */
export function useDebounced<T>(value: T, delay = 200): T {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);

  return settled;
}
