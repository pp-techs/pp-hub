import { getSchema, type JSONContent } from "@tiptap/core";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { serializeMarkdown } from "@slash-editor/core/markdown";
import { yXmlFragmentToProsemirrorJSON } from "@tiptap/y-tiptap";
import { expect, test } from "vite-plus/test";
import * as Y from "yjs";
import { createMdocKit } from "../src/lib/collab/mdoc-kit.ts";

/**
 * The server seeds collaboration rooms with Y updates built in Go
 * (`mdoc.Seed(mdoc.Parse(markdown))`). Each `testdata/go-updates/<name>.b64`
 * is that update (base64) for the golden fixture `<name>` (regenerate with
 * `go test ./internal/mdoc/ -run TestGoUpdates -update`). The editor must
 * read it as a valid document equal to what slash-editor itself parses from
 * the same markdown, and write the same markdown back.
 *
 * Comment marks are persisted by the Go side only: for each comment fixture
 * `testdata/comments/<name>.json`, `comment-<name>.b64` is Go's update of the
 * document and `comment-<name>.md` Go's markdown for it. The editor must read
 * the marks back with y-tiptap's own Y attribute keys, and the markdown must
 * be slash-editor's with the comment markers inserted.
 */

type Fixture = { name: string; json: unknown; output: string };
type CommentFixture = { name: string; json: unknown; js_output: string; yupdate_b64: string };
type PMNode = {
  type: string;
  attrs?: Record<string, unknown>;
  content?: PMNode[];
  marks?: JSONContent["marks"];
  [key: string]: unknown;
};

// Read through Vite's glob, as catalogs.test.ts does: the app's tsconfig has
// no Node types (hence the updates are stored base64).
const FIXTURES = import.meta.glob("../../server/internal/mdoc/testdata/fixtures/*.json", {
  import: "default",
  eager: true,
}) as Record<string, Fixture>;
const COMMENT_FIXTURES = import.meta.glob("../../server/internal/mdoc/testdata/comments/*.json", {
  import: "default",
  eager: true,
}) as Record<string, CommentFixture>;
const GO_UPDATES = import.meta.glob("../../server/internal/mdoc/testdata/go-updates/*.b64", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;
const GO_MARKDOWN = import.meta.glob("../../server/internal/mdoc/testdata/go-updates/*.md", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const fixtures = Object.values(FIXTURES).sort((a, b) => a.name.localeCompare(b.name));
const commentFixtures = Object.values(COMMENT_FIXTURES).sort((a, b) =>
  a.name.localeCompare(b.name),
);
/** `<name>` of `…/<name><ext>`. */
const baseName = (file: string, ext: string) => file.slice(file.lastIndexOf("/") + 1, -ext.length);
const fromBase64 = (base64: string) => Uint8Array.from(atob(base64.trim()), (c) => c.charCodeAt(0));
const goUpdates = Object.fromEntries(
  Object.entries(GO_UPDATES).map(([file, base64]) => [baseName(file, ".b64"), fromBase64(base64)]),
);
const goMarkdown = Object.fromEntries(
  Object.entries(GO_MARKDOWN).map(([file, markdown]) => [baseName(file, ".md"), markdown]),
);

const kit = createMdocKit();
const schema = getSchema(kit);

/** Drops block ids; a mention's `id` is the mentioned entity and stays. */
function stripIds(node: PMNode): PMNode {
  const { attrs, content, ...rest } = node;
  const out: PMNode = { ...rest };
  if (attrs) {
    const { id: _id, ...other } = attrs;
    out.attrs = node.type === "mention" ? attrs : other;
  }
  if (content) out.content = content.map(stripIds);
  return out;
}

function collectIds(node: PMNode, ids: unknown[]): unknown[] {
  if (node.type !== "mention" && node.attrs && "id" in node.attrs) ids.push(node.attrs.id);
  for (const child of node.content ?? []) collectIds(child, ids);
  return ids;
}

/** Overlapping marks (comments) compare as a set. */
function sortMarks(node: PMNode): PMNode {
  const out: PMNode = { ...node };
  if (node.marks) {
    out.marks = [...node.marks].sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)));
  }
  if (node.content) out.content = node.content.map(sortMarks);
  return out;
}

/** `node.check()`'s complaint, or null for a schema-valid node. */
function checkError(node: ProseMirrorNode): string | null {
  try {
    node.check();
    return null;
  } catch (error) {
    return String(error);
  }
}

/** The editor's document for a Go update, without Go's trailing empty paragraph. */
function readGoUpdate(update: Uint8Array): { doc: Y.Doc; node: ProseMirrorNode; body: PMNode } {
  const doc = new Y.Doc();
  Y.applyUpdate(doc, update);
  const node = schema.nodeFromJSON(yXmlFragmentToProsemirrorJSON(doc.getXmlFragment("content")));
  const json = node.toJSON() as PMNode;
  const ids = collectIds(json, []);
  expect(ids.every((id) => typeof id === "string" && /^[\w-]{12}$/.test(id))).toBe(true);
  expect(new Set(ids).size).toBe(ids.length);

  const last = json.content?.at(-1);
  expect(last?.type).toBe("paragraph");
  expect(last?.content).toBeUndefined();
  const body: PMNode = { ...json, content: json.content?.slice(0, -1) };
  if (body.content?.length === 0) delete body.content;
  return { doc, node, body };
}

/** Each Y text insert's attribute keys, in document order. */
function textAttributeKeys(fragment: Y.XmlFragment | Y.XmlElement): string[][] {
  return fragment
    .toArray()
    .flatMap((child) =>
      child instanceof Y.XmlText
        ? (child.toDelta() as { attributes?: Record<string, unknown> }[]).map((d) =>
            Object.keys(d.attributes ?? {}).sort(),
          )
        : child instanceof Y.XmlElement
          ? textAttributeKeys(child)
          : [],
    );
}

test("fixtures and Go updates exist", () => {
  expect(fixtures.length).toBeGreaterThan(0);
  expect(commentFixtures.length).toBeGreaterThan(0);
  expect(Object.keys(goUpdates).sort()).toEqual(
    [
      ...fixtures.map((fixture) => fixture.name),
      ...commentFixtures.map((fixture) => `comment-${fixture.name}`),
    ].sort(),
  );
  expect(Object.keys(goMarkdown).sort()).toEqual(
    commentFixtures.map((fixture) => `comment-${fixture.name}`).sort(),
  );
});

test.each(fixtures)("Go update $name reads as slash-editor's document", (fixture) => {
  const { node, body } = readGoUpdate(goUpdates[fixture.name]);
  const expectedNode = schema.nodeFromJSON(fixture.json);
  // The Go document must be schema-valid unless slash-editor's own parse of
  // the same markdown is invalid in the same way: parseMarkdown yields link +
  // code for `[`code`](url)` although code excludes every other mark, and
  // the Go side reproduces it rather than silently dropping the link. (An
  // empty parse is invalid, `doc` needs a block; the Go document always ends
  // with an empty paragraph.)
  expect([null, checkError(expectedNode)]).toContain(checkError(node));

  expect(stripIds(body)).toEqual(stripIds(expectedNode.toJSON() as PMNode));
  expect(serializeMarkdown(body, kit)).toBe(fixture.output);
});

test.each(commentFixtures)("Go update comment-$name keeps its comment marks", (fixture) => {
  const { doc, node, body } = readGoUpdate(goUpdates[`comment-${fixture.name}`]);
  expect(checkError(node)).toBeNull();
  const expectedNode = schema.nodeFromJSON(fixture.json);
  expect(sortMarks(stripIds(body))).toEqual(sortMarks(stripIds(expectedNode.toJSON() as PMNode)));

  const jsDoc = new Y.Doc();
  Y.applyUpdate(jsDoc, fromBase64(fixture.yupdate_b64));
  expect(textAttributeKeys(doc.getXmlFragment("content"))).toEqual(
    textAttributeKeys(jsDoc.getXmlFragment("content")),
  );

  const markdown = goMarkdown[`comment-${fixture.name}`];
  expect(markdown.replace(/<!-- \/?slash:comment \{.*?\} -->/g, "")).toBe(fixture.js_output);
  expect(serializeMarkdown(body, kit)).toBe(fixture.js_output);
});
