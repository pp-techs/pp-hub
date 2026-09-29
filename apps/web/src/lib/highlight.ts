/** A run of text, and whether it is part of a match. */
export interface TextSegment {
  text: string;
  hit: boolean;
}

/**
 * Splits `text` into plain and highlighted runs.
 *
 * `ranges` are `[start, end)` offsets in code points — the server counts
 * runes, and a JavaScript string index counts UTF-16 units, so slicing the
 * string directly would cut an emoji or any astral character in half and
 * shift every later highlight by one. Ranges are clamped to the text,
 * sorted and merged, so an out-of-order or overlapping answer still renders
 * each character once.
 */
export function highlightSegments(
  text: string,
  ranges: readonly (readonly [number, number])[] | undefined,
): TextSegment[] {
  const chars = Array.from(text);
  const spans = (ranges ?? [])
    .map(([start, end]): [number, number] => [
      Math.max(0, Math.min(start, chars.length)),
      Math.max(0, Math.min(end, chars.length)),
    ])
    .filter(([start, end]) => end > start)
    .sort((a, b) => a[0] - b[0]);

  const merged: [number, number][] = [];
  for (const span of spans) {
    const last = merged.at(-1);
    if (last && span[0] <= last[1]) last[1] = Math.max(last[1], span[1]);
    else merged.push([span[0], span[1]]);
  }

  const segments: TextSegment[] = [];
  let cursor = 0;
  for (const [start, end] of merged) {
    if (start > cursor) segments.push({ text: chars.slice(cursor, start).join(""), hit: false });
    segments.push({ text: chars.slice(start, end).join(""), hit: true });
    cursor = end;
  }
  if (cursor < chars.length) segments.push({ text: chars.slice(cursor).join(""), hit: false });
  return segments;
}
