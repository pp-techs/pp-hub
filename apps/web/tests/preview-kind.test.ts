import { expect, test } from "vite-plus/test";
import { previewKind } from "../src/lib/api/utils.ts";

/**
 * previewKind must return exactly the kind the worker stored the blob under.
 * Any drift from kindForMime in apps/server/internal/preview/preview.go makes
 * GET /nodes/:id/preview?kind=… 404 with "preview not ready".
 * See docs/architecture/preview-pipeline.md.
 */
const CONTRACT: [string | undefined, ReturnType<typeof previewKind>][] = [
  ["application/pdf", "pdf"],
  ["image/png", "thumbnail"],
  ["image/jpeg", "thumbnail"],
  ["image/gif", "thumbnail"],
  ["image/webp", "thumbnail"],
  ["video/mp4", "video"],
  ["video/webm", "video"],
  ["video/quicktime", "video"],
  ["application/rtf", "text"],
  ["text/plain", "text"],
  ["text/markdown", "text"],
  ["text/csv; charset=utf-8", "text"],
  // No job kind server-side → no preview row → must not be requested.
  ["application/epub+zip", null],
  ["application/vnd.openxmlformats-officedocument.wordprocessingml.document", null],
  ["image/svg+xml", null],
  ["video/x-matroska", null],
  ["application/zip", null],
  [undefined, null],
];

test.each(CONTRACT)("previewKind(%s) === %s", (mime, expected) => {
  expect(previewKind(mime)).toBe(expected);
});
