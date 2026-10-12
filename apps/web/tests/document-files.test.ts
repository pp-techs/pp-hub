import { expect, test } from "vite-plus/test";
import { documentFilesResolver, isRelativeRef } from "../src/lib/collab/document-files.ts";

// What a media block's `src` turns into: the document's attachment, a file
// beside it by relative path, or the src as written. The server
// (`mdoc.RelativePath`) holds the authoritative rule; this one must never
// send it an attachment, a URL or a rooted path.
const files = {
  attachment: (hash: string) => `att(${hash})`,
  relative: (path: string) => `rel(${path})`,
  openRelative: () => {},
};
const resolve = documentFilesResolver(files);

const CONTRACT: [src: string, resolved: string][] = [
  ["attachment:abc", "att(abc)"],
  ["images/a.png", "rel(images/a.png)"],
  ["./a.png", "rel(./a.png)"],
  ["../shared/Bùi đẹp.png", "rel(../shared/Bùi đẹp.png)"],
  ["./a:b.png", "rel(./a:b.png)"],
  ["a.png?v=2#x", "rel(a.png?v=2#x)"],
  ["https://example.com/a.png", "https://example.com/a.png"],
  ["//cdn.example/a.png", "//cdn.example/a.png"],
  ["/edit/abc", "/edit/abc"],
  ["data:image/png;base64,xx", "data:image/png;base64,xx"],
  ["mailto:a@b.c", "mailto:a@b.c"],
  ["#heading", "#heading"],
  ["images/", "images/"],
  ["..", ".."],
];

test.each(CONTRACT)("%s resolves to %s", (src, resolved) => {
  expect(resolve(src, null as never)).toBe(resolved);
});

test("only a relative path is a relative reference", () => {
  expect(isRelativeRef("../Spec.md#scope")).toBe(true);
  expect(isRelativeRef(null)).toBe(false);
  expect(isRelativeRef("attachment:abc")).toBe(false);
  expect(isRelativeRef("C:\\a.png")).toBe(false);
});
