import { expect, test } from "vite-plus/test";
import { canOpenInTab } from "../src/lib/api/utils.ts";
import type { Node } from "../src/lib/api/types.ts";

const file = (mime?: string): Node => ({ id: "n1", name: "f", kind: "file", mime }) as Node;

/**
 * canOpenInTab must match inlineMimes in apps/server/internal/httpapi/drive.go:
 * the server downgrades anything else to an attachment, so offering "Open in
 * new tab" for it would trigger a download instead.
 */
const CONTRACT: [Node, boolean][] = [
  [file("application/pdf"), true],
  [file("image/png"), true],
  [file("video/mp4"), true],
  [file("text/plain; charset=utf-8"), true],
  // Same-origin executables are never served inline.
  [file("text/html"), false],
  [file("image/svg+xml"), false],
  [file("application/octet-stream"), false],
  [file(undefined), false],
  [{ id: "d1", name: "d", kind: "dir" } as Node, false],
];

test.each(CONTRACT)("canOpenInTab(%o) === %s", (node, expected) => {
  expect(canOpenInTab(node)).toBe(expected);
});
