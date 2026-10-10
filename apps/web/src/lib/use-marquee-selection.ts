import { useCallback, useEffect, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent, RefObject } from "react";

/** The band, in the container's own content coordinates. */
export interface MarqueeRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

export interface MarqueeOptions {
  /** Off while a secondary panel has replaced the listing. */
  enabled?: boolean;
  /** A press landed on the background: remember the selection to add to. */
  onStart: () => void;
  /**
   * The ids the band covers. The band is in the container's content
   * coordinates; the listing answers from its own row geometry, since a
   * virtualized listing has most of its nodes unmounted.
   */
  hitTest: (band: MarqueeRect) => Set<string>;
  /** The ids the band covers; `additive` when ⌘/Ctrl/Shift is held. */
  onChange: (ids: Set<string>, additive: boolean) => void;
}

/** Pointer travel, in px, before a press counts as a drag rather than a click. */
const THRESHOLD = 4;

/**
 * Rubber-band selection over a scrolling listing.
 *
 * A press on the background — anything that is not a node (`[data-node-id]`)
 * or a control that acts on the selection (`[data-keeps-selection]`) — starts
 * a band. Released without moving, it is an ordinary background click and
 * reports an empty selection, which is what clears one.
 *
 * The band is kept in the container's content coordinates rather than the
 * viewport's, so scrolling mid-drag leaves it anchored to the nodes it was
 * drawn over instead of sliding with the wheel. Which nodes it covers is the
 * listing's `hitTest` to answer, and only a change in that set is reported.
 */
export function useMarqueeSelection(
  container: RefObject<HTMLElement | null>,
  options: MarqueeOptions,
): { onMouseDown: (event: ReactMouseEvent) => void; rect: MarqueeRect | null } {
  const [rect, setRect] = useState<MarqueeRect | null>(null);
  const cleanupRef = useRef<(() => void) | null>(null);
  useEffect(() => {
    return () => {
      cleanupRef.current?.();
    };
  }, []);
  // The handler outlives the render that installed it, so it reads the
  // callbacks through a ref rather than closing over stale ones.
  const latest = useRef(options);
  latest.current = options;
  const onMouseDown = useCallback(
    (event: ReactMouseEvent) => {
      const el = container.current;
      if (!el || latest.current.enabled === false || event.button !== 0) return;
      if ((event.target as HTMLElement).closest("[data-node-id],[data-keeps-selection]")) return;

      // Otherwise the browser starts a text selection across the listing.
      event.preventDefault();

      const bounds = el.getBoundingClientRect();
      const point = (e: { clientX: number; clientY: number }) => ({
        x: e.clientX - bounds.left + el.scrollLeft,
        y: e.clientY - bounds.top + el.scrollTop,
      });
      const start = point(event);
      let dragged = false;
      // What was last reported: a move that covers the same nodes changes
      // nothing, so it must not cost the listing a re-render.
      let last: Set<string> | null = null;
      latest.current.onStart();

      const move = (e: MouseEvent) => {
        const now = point(e);
        const band = {
          left: Math.min(start.x, now.x),
          top: Math.min(start.y, now.y),
          width: Math.abs(now.x - start.x),
          height: Math.abs(now.y - start.y),
        };
        if (!dragged && Math.max(band.width, band.height) < THRESHOLD) return;
        dragged = true;
        setRect(band);
        const ids = latest.current.hitTest(band);
        const prev = last;
        if (prev && ids.size === prev.size && [...ids].every((id) => prev.has(id))) return;
        last = ids;
        latest.current.onChange(ids, e.shiftKey || e.metaKey || e.ctrlKey);
      };
      const up = (e: MouseEvent) => {
        cleanupRef.current = null;
        // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM event name
        window.removeEventListener("mousemove", move);
        // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM event name
        window.removeEventListener("mouseup", up);
        setRect(null);
        if (!dragged) latest.current.onChange(new Set(), e.shiftKey || e.metaKey || e.ctrlKey);
      };

      cleanupRef.current = () => {
        // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM event name
        window.removeEventListener("mousemove", move);
        // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM event name
        window.removeEventListener("mouseup", up);
      };
      // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM event name
      window.addEventListener("mousemove", move);
      // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM event name
      window.addEventListener("mouseup", up);
    },
    [container],
  );

  return { onMouseDown, rect };
}
