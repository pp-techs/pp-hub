import { expect, test } from "vite-plus/test";
import type { Node } from "../src/lib/api/types.ts";
import {
  canPreview,
  hasThumbnail,
  previewKind,
  previewUrl,
  thumbnailFit,
} from "../src/lib/api/utils.ts";

/**
 * previewKind returns the *artifact* the server stored, not the job kind that
 * produced it. Images, PDFs and video all rasterize to one thumbnail, so they
 * all answer "thumbnail" — the client no longer mirrors kindForMime.
 * See docs/architecture/preview-pipeline.md.
 */
const CONTRACT: [string | undefined, ReturnType<typeof previewKind>][] = [
  ["application/pdf", "thumbnail"],
  ["image/png", "thumbnail"],
  ["image/jpeg", "thumbnail"],
  ["image/gif", "thumbnail"],
  ["image/webp", "thumbnail"],
  ["video/mp4", "thumbnail"],
  ["video/webm", "thumbnail"],
  ["video/quicktime", "thumbnail"],
  ["application/rtf", "text"],
  ["text/plain", "text"],
  ["text/markdown", "text"],
  ["text/csv; charset=utf-8", "text"],
  // Office docs convert to PDF and rasterize, so they thumbnail too.
  ["application/vnd.openxmlformats-officedocument.wordprocessingml.document", "thumbnail"],
  ["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "thumbnail"],
  ["application/vnd.openxmlformats-officedocument.presentationml.presentation", "thumbnail"],
  ["application/msword", "thumbnail"],
  // No job kind server-side → no preview row → must not be requested.
  // .epub is a zip the editor opens, but a rendered "cover" would misrepresent
  // it; .csv keeps its text snippet rather than becoming a spreadsheet render.
  ["application/epub+zip", null],
  ["text/csv", "text"],
  ["image/svg+xml", null],
  ["video/x-matroska", null],
  ["application/zip", null],
  [undefined, null],
];

test.each(CONTRACT)("previewKind(%s) === %s", (mime, expected) => {
  expect(previewKind(mime)).toBe(expected);
});

function file(mime: string | undefined, version = 1): Node {
  return { id: "n1", space_id: "s1", kind: "file", name: "f", mime, version };
}

test("hasThumbnail covers every rasterized type", () => {
  for (const mime of [
    "application/pdf",
    "image/png",
    "video/mp4",
    "video/quicktime",
    "image/webp",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    "application/vnd.oasis.opendocument.presentation",
  ]) {
    expect(hasThumbnail(file(mime))).toBe(true);
  }
});

test("hasThumbnail is false for text and unrenderable types", () => {
  for (const mime of [
    "text/plain",
    "application/zip",
    "image/svg+xml",
    "application/epub+zip",
    undefined,
  ]) {
    expect(hasThumbnail(file(mime))).toBe(false);
  }
});

test("folders never have thumbnails", () => {
  const dir: Node = { id: "d1", space_id: "s1", kind: "dir", name: "d" };
  expect(hasThumbnail(dir)).toBe(false);
});

/**
 * The version has to be in the URL: the response is cached `immutable` for a
 * year, so without it an edited file keeps serving the superseded thumbnail
 * out of every viewer's cache.
 */
test("previewUrl is version-keyed so immutable caching can't pin a stale image", () => {
  expect(previewUrl(file("image/png", 1))).toContain("v=1");
  expect(previewUrl(file("image/png", 1))).not.toBe(previewUrl(file("image/png", 2)));
});

test("previewUrl defaults to the thumbnail kind", () => {
  expect(previewUrl(file("image/png"))).toContain("kind=thumbnail");
  expect(previewUrl(file("text/plain"), "text")).toContain("kind=text");
});

/**
 * canPreview now means "the modal can show something", which includes types
 * the browser renders inline from /content even when no blob was generated.
 */
test("canPreview includes inline-renderable types", () => {
  expect(canPreview(file("application/pdf"))).toBe(true);
  expect(canPreview(file("video/mp4"))).toBe(true);
  expect(canPreview(file("audio/mpeg"))).toBe(true); // inline, no preview blob
  expect(canPreview(file("text/plain"))).toBe(true);
  expect(canPreview(file("application/zip"))).toBe(false);
});

/**
 * Pictures crop to fill their frame; pages letterbox. The PDF case is the one
 * that decides the rule: it renders through the image pipeline but is a page,
 * and cropping a portrait A4 to a landscape card shows a strip of its middle.
 */
test("thumbnailFit crops pictures", () => {
  for (const mime of ["image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4"]) {
    expect(thumbnailFit(file(mime))).toBe("cover");
  }
});

test("thumbnailFit letterboxes pages", () => {
  for (const mime of [
    "application/pdf",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    "application/vnd.oasis.opendocument.presentation",
  ]) {
    expect(thumbnailFit(file(mime))).toBe("contain");
  }
});

test("thumbnailFit handles mime parameters and absent mime", () => {
  expect(thumbnailFit(file("image/jpeg; charset=binary"))).toBe("cover");
  // No mime means no thumbnail either, so the value is unused — it just must
  // not throw or crop something unknown.
  expect(thumbnailFit(file(undefined))).toBe("contain");
});
