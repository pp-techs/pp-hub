import { getSchema } from "@tiptap/core";
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
 */

type Fixture = { name: string; json: unknown; output: string };
type PMNode = {
  type: string;
  attrs?: Record<string, unknown>;
  content?: PMNode[];
  [key: string]: unknown;
};

// Read through Vite's glob, as catalogs.test.ts does: the app's tsconfig has
// no Node types (hence the updates are stored base64).
const FIXTURES = import.meta.glob("../../server/internal/mdoc/testdata/fixtures/*.json", {
  import: "default",
  eager: true,
}) as Record<string, Fixture>;
const GO_UPDATES = import.meta.glob("../../server/internal/mdoc/testdata/go-updates/*.b64", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const fixtures = Object.values(FIXTURES).sort((a, b) => a.name.localeCompare(b.name));
const goUpdates = Object.fromEntries(
  Object.entries(GO_UPDATES).map(([file, base64]) => [
    file.slice(file.lastIndexOf("/") + 1, -".b64".length),
    Uint8Array.from(atob(base64.trim()), (c) => c.charCodeAt(0)),
  ]),
);

const kit = createMdocKit();
const schema = getSchema(kit);

function stripIds(node: PMNode): PMNode {
  const { attrs, content, ...rest } = node;
  const out: PMNode = { ...rest };
  if (attrs) {
    const { id: _id, ...other } = attrs;
    out.attrs = other;
  }
  if (content) out.content = content.map(stripIds);
  return out;
}

function collectIds(node: PMNode, ids: unknown[]): unknown[] {
  if (node.attrs && "id" in node.attrs) ids.push(node.attrs.id);
  for (const child of node.content ?? []) collectIds(child, ids);
  return ids;
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

test("fixtures and Go updates exist", () => {
  expect(fixtures.length).toBeGreaterThan(0);
  expect(Object.keys(goUpdates).sort()).toEqual(fixtures.map((fixture) => fixture.name).sort());
});

test.each(fixtures)("Go update $name reads as slash-editor's document", (fixture) => {
  const doc = new Y.Doc();
  Y.applyUpdate(doc, goUpdates[fixture.name]);
  const node = schema.nodeFromJSON(yXmlFragmentToProsemirrorJSON(doc.getXmlFragment("content")));
  const expectedNode = schema.nodeFromJSON(fixture.json);
  // The Go document must be schema-valid unless slash-editor's own parse of
  // the same markdown is invalid in the same way: parseMarkdown yields link +
  // code for `[`code`](url)` although code excludes every other mark, and
  // the Go side reproduces it rather than silently dropping the link. (An
  // empty parse is invalid, `doc` needs a block; the Go document always ends
  // with an empty paragraph.)
  expect([null, checkError(expectedNode)]).toContain(checkError(node));

  const json = node.toJSON() as PMNode;
  const ids = collectIds(json, []);
  expect(ids.every((id) => typeof id === "string" && /^[\w-]{12}$/.test(id))).toBe(true);
  expect(new Set(ids).size).toBe(ids.length);

  const last = json.content?.at(-1);
  expect(last?.type).toBe("paragraph");
  expect(last?.content).toBeUndefined();
  const body: PMNode = { ...json, content: json.content?.slice(0, -1) };
  if (body.content?.length === 0) delete body.content;

  expect(stripIds(body)).toEqual(stripIds(expectedNode.toJSON() as PMNode));
  expect(serializeMarkdown(body, kit)).toBe(fixture.output);
});
