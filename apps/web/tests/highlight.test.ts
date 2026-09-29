import { expect, test } from "vite-plus/test";
import { highlightSegments } from "../src/lib/highlight.ts";

const render = (text: string, ranges?: [number, number][]) =>
  highlightSegments(text, ranges)
    .map((s) => (s.hit ? `[${s.text}]` : s.text))
    .join("");

test("highlights code-point ranges, including after astral characters", () => {
  // "😀" is two UTF-16 units but one rune: a string-index slice would cut
  // "b" in half and shift every later highlight by one.
  expect(
    render("😀 abc tiếng Việt", [
      [2, 5],
      [6, 11],
    ]),
  ).toBe("😀 [abc] [tiếng] Việt");
  expect(render("…see the plan…", [[9, 13]])).toBe("…see the [plan]…");
});

test("no ranges leaves the text as one plain run", () => {
  expect(highlightSegments("plain", undefined)).toEqual([{ text: "plain", hit: false }]);
  expect(highlightSegments("", [[0, 3]])).toEqual([]);
});

test("unsorted, overlapping and out-of-bounds ranges render each character once", () => {
  expect(
    render("abcdefgh", [
      [5, 20],
      [1, 3],
      [2, 4],
      [-4, 0],
      [6, 6],
    ]),
  ).toBe("a[bcd]e[fgh]");
  expect(
    render("abcdef", [
      [0, 2],
      [2, 4],
    ]),
  ).toBe("[abcd]ef");
});
