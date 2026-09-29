/**
 * Golden fixtures for `apps/server/internal/mdoc`: holds the Go markdown ↔
 * ProseMirror ↔ Y.XmlFragment conversion to slash-editor's own behaviour.
 *
 *   bun apps/web/scripts/mdoc-fixtures.ts        (from the repo root)
 *
 * For every case it writes `apps/server/internal/mdoc/testdata/fixtures/<name>.json`:
 *
 *   - `input`       the markdown file
 *   - `frontmatter` its leading YAML frontmatter block ("" if none), which
 *                   the server splits off before parsing (slash-editor has
 *                   no frontmatter support) and prepends when rendering
 *   - `json`        `parseMarkdown(input minus frontmatter, kit)`
 *   - `output`      `serializeMarkdown(json, kit)`
 *   - `yupdate_b64` a V1 update of a fresh Y.Doc whose fragment `content`
 *                   holds `json`, written by `@tiptap/y-tiptap`'s
 *                   `prosemirrorJSONToYXmlFragment`
 *   - `expect_lossy` whether `mdoc.Parse` must flag the input
 *
 * plus `schema.json` (node/mark attribute defaults, id-bearing node types,
 * mark rank order) which the Go schema table is checked against.
 *
 * The kit is `createMdocKit()` from `src/lib/collab/mdoc-kit.ts`, the same
 * v1 schema the editor mounts. Output is deterministic: rerunning it on the
 * same `@slash-editor/core` version changes nothing (Y client ids are pinned).
 */
import { mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { getSchema } from "@tiptap/core";
import { parseMarkdown, serializeMarkdown } from "@slash-editor/core/markdown";
import { prosemirrorJSONToYXmlFragment } from "@tiptap/y-tiptap";
import * as Y from "yjs";
import { createMdocKit } from "../src/lib/collab/mdoc-kit.ts";

type Case = { name: string; input: string; frontmatter?: string; lossy?: boolean };

const cases: Case[] = [
  // Empty and whitespace
  { name: "empty", input: "" },
  { name: "only-newlines", input: "\n\n\n" },
  // Paragraphs and blank-line runs (implicit empty paragraphs)
  { name: "paragraph", input: "Hello world" },
  { name: "paragraph-trailing-newline", input: "Hello world\n" },
  { name: "paragraphs", input: "First paragraph.\n\nSecond paragraph.\n" },
  { name: "paragraph-soft-break", input: "line one\nline two\n" },
  { name: "blank-lines-2", input: "a\n\n\nb\n" },
  { name: "blank-lines-3", input: "a\n\n\n\nb\n" },
  { name: "blank-lines-5", input: "a\n\n\n\n\n\nb\n" },
  { name: "leading-blank-lines", input: "\n\na\n" },
  { name: "trailing-blank-lines", input: "a\n\n\n" },
  { name: "nbsp-paragraphs", input: "a\n\n\n\n&nbsp;\n\nb\n" },
  // marked keeps line indentation and trailing whitespace as text
  { name: "paragraph-indented-lines", input: "  a\n   b\n\nc\n  d\n" },
  { name: "paragraph-trailing-spaces", input: "a  \n\nb \n" },
  // Headings
  { name: "headings", input: "# H1\n\n## H2\n\n### H3\n\n#### H4\n\n##### H5\n\n###### H6\n" },
  { name: "heading-blank-lines", input: "# Title\n\n\n\nbody\n" },
  { name: "heading-marks", input: "## A **bold** and `code` title\n" },
  // Marks
  { name: "marks-basic", input: "Some **bold**, *italic*, ~~strike~~ and `code`.\n" },
  { name: "marks-underscore", input: "__bold__ and _italic_\n" },
  { name: "marks-nested", input: "**bold *both* bold** and *italic **both***\n" },
  { name: "marks-bold-italic", input: "***both***\n" },
  { name: "marks-strike-bold", input: "~~**x**~~ and **~~y~~**\n" },
  { name: "link", input: "A [link](https://example.com) here.\n" },
  { name: "link-title", input: 'A [link](https://example.com "The title") here.\n' },
  {
    name: "link-bold",
    input: "**[bold link](https://example.com)** and [**inner**](https://x.y)\n",
  },
  { name: "link-code", input: "[`code link`](https://example.com)\n" },
  { name: "link-autolink", input: "See <https://example.com/a?b=1>.\n" },
  { name: "link-bare-url", input: "Visit https://example.com today.\n" },
  { name: "link-adjacent", input: "[a](https://a.example)[b](https://b.example)\n" },
  {
    name: "link-bare-punctuated",
    input: 'Say "https://example.com/a", (www.example.com). or [a@b.io]; x:https://e.x/y_(z).\n',
  },
  { name: "link-bare-in-label", input: "[https://x.example](https://y.example)\n" },
  { name: "code-special", input: "`a*b_c` and `` x`y ``\n" },
  { name: "mark-whitespace", input: "a ** b** c\n" },
  // Escaping
  {
    name: "escape-specials",
    input: "\\*not bold\\* \\_x\\_ \\`y\\` \\[z\\] a\\~b back\\\\slash\n",
  },
  { name: "escape-html-chars", input: "a < b > c & d\n" },
  { name: "escape-entities", input: "&lt;tag&gt; &amp; &quot;q&quot;\n" },
  { name: "escape-hash", input: "\\# not a heading\n" },
  { name: "unicode", input: "Tiếng Việt, 日本語, emoji 🎉 and \u00e9\n" },
  // Hard breaks
  { name: "hard-break-spaces", input: "line one  \nline two\n" },
  { name: "hard-break-backslash", input: "line one\\\nline two\n" },
  { name: "hard-break-in-bold", input: "**a  \nb**\n" },
  { name: "hard-break-indented", input: "a  \n   b\n" },
  // Lists
  { name: "bullet-list", input: "- one\n- two\n- three\n" },
  { name: "bullet-list-star", input: "* one\n* two\n" },
  { name: "bullet-list-nested", input: "- one\n  - nested\n    - deeper\n- two\n" },
  { name: "bullet-list-marks", input: "- **bold** item\n- [link](https://x.y)\n" },
  { name: "bullet-list-loose", input: "- one\n\n- two\n" },
  { name: "bullet-list-multi-paragraph", input: "- one\n\n  continued\n- two\n" },
  { name: "ordered-list", input: "1. one\n2. two\n3. three\n" },
  { name: "ordered-list-start", input: "3. three\n4. four\n" },
  { name: "ordered-list-paren", input: "1) one\n2) two\n" },
  { name: "ordered-list-nested", input: "1. one\n   1. nested\n   2. nested two\n2. two\n" },
  { name: "ordered-in-bullet", input: "- one\n  1. first\n  2. second\n- two\n" },
  { name: "bullet-in-ordered", input: "1. one\n   - a\n   - b\n2. two\n" },
  {
    name: "ordered-list-ten",
    input: "1. a\n2. b\n3. c\n4. d\n5. e\n6. f\n7. g\n8. h\n9. i\n10. j\n",
  },
  { name: "task-list", input: "- [ ] todo\n- [x] done\n" },
  {
    name: "task-list-nested",
    input: "- [ ] parent\n  - [x] child\n  - [ ] child two\n- [ ] next\n",
  },
  { name: "task-list-marks", input: "- [x] **bold** task\n" },
  { name: "task-list-blank-lines", input: "- [ ] a\n\n\n\nb\n" },
  { name: "mixed-task-bullet", input: "- [ ] task\n- plain\n" },
  { name: "list-then-paragraph", input: "- a\n- b\n\nafter\n" },
  { name: "list-item-trailing-spaces", input: "- a  \n- b  \n  - c  \n  - d  \n" },
  { name: "list-item-continuation", input: "- a\n   b\n- c\n" },
  { name: "list-item-alpha-interrupt", input: "- foo\n  on. bar\n- baz\n" },
  { name: "ordered-list-continuation", input: "1. foo\n   bar\n2. **x**\n    y\n" },
  // Blockquote
  { name: "blockquote", input: "> quoted\n" },
  { name: "blockquote-multi", input: "> first\n>\n> second\n" },
  { name: "blockquote-nested", input: "> outer\n>\n> > inner\n" },
  { name: "blockquote-list", input: "> - a\n> - b\n" },
  // Code blocks
  { name: "code-block", input: "```\nplain code\n```\n" },
  { name: "code-block-lang", input: "```ts\nconst x = 1;\n\nconsole.log(x);\n```\n" },
  { name: "code-block-tilde", input: "~~~python\nprint('hi')\n~~~\n" },
  { name: "code-block-empty", input: "```js\n```\n" },
  { name: "code-block-specials", input: "```md\n**not bold** <b> & `x`\n```\n" },
  { name: "code-block-indented", input: "    indented code\n    line two\n" },
  // Rule
  { name: "horizontal-rule", input: "above\n\n---\n\nbelow\n" },
  { name: "horizontal-rule-stars", input: "***\n" },
  // Tables
  { name: "table", input: "| a | b |\n| --- | --- |\n| c | d |\n" },
  {
    name: "table-align",
    input: "| left | center | right | none |\n| :--- | :---: | ---: | --- |\n| 1 | 2 | 3 | 4 |\n",
  },
  { name: "table-marks", input: "| **h** | `c` |\n| --- | --- |\n| [l](https://x.y) | *i* |\n" },
  {
    name: "table-padded",
    input: "| Name   | Value |\n|--------|-------|\n| x      | 1     |\n| longer | 22    |\n",
  },
  { name: "table-wide-unicode", input: "| Tên | 值 |\n| --- | --- |\n| Việt Nam | 中文字符 |\n" },
  { name: "table-then-paragraph", input: "| a |\n| --- |\n| b |\n\nafter\n" },
  // Callouts
  { name: "callout-note", input: "> [!NOTE]\n> A note.\n" },
  { name: "callout-tip", input: "> [!TIP]\n> A tip.\n" },
  { name: "callout-important", input: "> [!IMPORTANT]\n> Important.\n" },
  { name: "callout-warning", input: "> [!WARNING]\n> Careful.\n" },
  { name: "callout-caution", input: "> [!CAUTION]\n> Danger.\n" },
  { name: "callout-lowercase", input: "> [!note]\n> lower\n" },
  {
    name: "callout-custom-icon",
    input: '<!-- slash:callout {"icon":"🔥"} -->\n> [!NOTE]\n> Hot.\n',
  },
  { name: "callout-multi-block", input: "> [!TIP]\n> First.\n>\n> - a\n> - b\n" },
  { name: "callout-empty", input: "> [!NOTE]\n" },
  // Toggles
  {
    name: "details-closed",
    input: "<details>\n<summary>Title</summary>\n\nBody text.\n\n</details>\n",
  },
  {
    name: "details-open",
    input: "<details open>\n<summary>Open **title**</summary>\n\nBody.\n\n- a\n\n</details>\n",
  },
  {
    name: "details-heading-1",
    input: "<details>\n<summary><h1>Big</h1></summary>\n\nx\n\n</details>\n",
  },
  {
    name: "details-heading-2",
    input: "<details open>\n<summary><h2>Mid</h2></summary>\n\ny\n\n</details>\n",
  },
  {
    name: "details-heading-3",
    input: "<details>\n<summary><h3>Small</h3></summary>\n\nz\n\n</details>\n",
  },
  { name: "details-empty-body", input: "<details>\n<summary>Only title</summary>\n</details>\n" },
  {
    name: "details-nested",
    input:
      "<details>\n<summary>Outer</summary>\n\n<details>\n<summary>Inner</summary>\n\ndeep\n\n</details>\n\n</details>\n",
  },
  // Columns
  {
    name: "columns-two",
    input:
      "<!-- slash:columns -->\n\nleft\n\n<!-- slash:column -->\n\nright\n\n<!-- /slash:columns -->\n",
  },
  {
    name: "columns-three",
    input:
      "<!-- slash:columns -->\n\n# A\n\n<!-- slash:column -->\n\n- b\n\n<!-- slash:column -->\n\n> c\n\n<!-- /slash:columns -->\n",
  },
  // Mermaid
  { name: "mermaid", input: "```mermaid\ngraph TD\n  A --> B\n```\n" },
  // Images and files
  { name: "image", input: "![alt text](https://example.com/a.png)\n" },
  { name: "image-no-alt", input: "![](https://example.com/a.png)\n" },
  { name: "image-attachment", input: "![diagram](attachment:0123abcd)\n" },
  {
    name: "image-width",
    input: '<!-- slash:image {"width":320} -->\n![pic](attachment:0123abcd)\n',
  },
  { name: "image-spaces-url", input: "![a](<https://example.com/a b.png>)\n" },
  {
    name: "file",
    input:
      '<!-- slash:file {"size":2048,"mime":"application/pdf"} -->\n[report.pdf](attachment:beef01)\n',
  },
  { name: "file-no-attrs", input: "<!-- slash:file -->\n[notes.txt](attachment:beef02)\n" },
  // Frontmatter
  {
    name: "frontmatter",
    frontmatter: "---\ntitle: Spec\ntags: [a, b]\n---\n",
    input: "# Heading\n\nBody.\n",
  },
  { name: "frontmatter-only", frontmatter: "---\ntitle: x\n---\n", input: "" },
  { name: "frontmatter-empty", frontmatter: "---\n---\n", input: "Body.\n" },
  { name: "frontmatter-dots", frontmatter: "---\na: 1\n...\n", input: "\nBody.\n" },
  { name: "frontmatter-crlf", frontmatter: "---\r\na: 1\r\n---\r\n", input: "Body.\n" },
  { name: "rule-first", input: "---\n\nNot frontmatter.\n" },
  // A realistic document mixing everything
  {
    name: "kitchen-sink",
    input: [
      "# Runbook",
      "",
      'Intro with **bold**, *italic*, ~~gone~~, `code` and a [link](https://example.com "t").',
      "",
      "> [!WARNING]",
      "> Read this first.",
      "",
      "## Steps",
      "",
      "1. Prepare",
      "   - check disk",
      "   - check network",
      "2. Deploy",
      "",
      "- [x] staged",
      "- [ ] prod",
      "",
      "```bash",
      "make deploy",
      "```",
      "",
      "| Env | Status |",
      "| :-- | --: |",
      "| staging | ok |",
      "",
      "<details>",
      "<summary>Rollback</summary>",
      "",
      "Run `make rollback`.",
      "",
      "</details>",
      "",
      "---",
      "",
      "![arch](attachment:abc123)",
      "",
    ].join("\n"),
  },
  // Lossy: constructs outside the v1 block set
  { name: "lossy-html-inline", input: "Some <span>inline</span> html.\n", lossy: true },
  { name: "lossy-html-block", input: "<div>\nblock html\n</div>\n", lossy: true },
  { name: "lossy-footnote", input: "Text[^1].\n\n[^1]: The footnote.\n", lossy: true },
  { name: "lossy-subscript", input: "H~2~O\n", lossy: true },
  {
    name: "lossy-inline-image",
    input: "Text with ![img](https://x.y/a.png) inside.\n",
    lossy: true,
  },
];

const here = path.dirname(fileURLToPath(import.meta.url));
const outDir = path.resolve(here, "../../server/internal/mdoc/testdata/fixtures");
const schemaFile = path.resolve(here, "../../server/internal/mdoc/testdata/schema.json");

const kit = createMdocKit();
const schema = getSchema(kit);

function encodeUpdate(json: unknown): string {
  const doc = new Y.Doc();
  doc.clientID = 1;
  prosemirrorJSONToYXmlFragment(schema, json, doc.getXmlFragment("content"));
  return Buffer.from(Y.encodeStateAsUpdate(doc)).toString("base64");
}

function attrDefaults(spec: Record<string, { default?: unknown }> | undefined) {
  return Object.fromEntries(
    Object.entries(spec ?? {}).map(([key, attr]) => [
      key,
      attr.default === undefined ? null : attr.default,
    ]),
  );
}

rmSync(outDir, { recursive: true, force: true });
mkdirSync(outDir, { recursive: true });
const seen = new Set<string>();
for (const { name, input, frontmatter = "", lossy = false } of cases) {
  if (seen.has(name)) throw new Error(`duplicate fixture name ${name}`);
  seen.add(name);
  const json = parseMarkdown(input, kit);
  const output = serializeMarkdown(json, kit);
  const fixture = {
    name,
    input: frontmatter + input,
    frontmatter,
    json,
    output,
    yupdate_b64: encodeUpdate(json),
    expect_lossy: lossy,
  };
  writeFileSync(path.join(outDir, `${name}.json`), `${JSON.stringify(fixture, null, 2)}\n`);
}

writeFileSync(
  schemaFile,
  `${JSON.stringify(
    {
      nodes: Object.fromEntries(
        Object.values(schema.nodes).map((type) => [
          type.name,
          attrDefaults(type.spec.attrs as never),
        ]),
      ),
      marks: Object.fromEntries(
        Object.values(schema.marks).map((type) => [
          type.name,
          attrDefaults(type.spec.attrs as never),
        ]),
      ),
      mark_order: Object.values(schema.marks).map((type) => type.name),
    },
    null,
    2,
  )}\n`,
);

console.log(
  `wrote ${readdirSync(outDir).length} fixtures to ${path.relative(process.cwd(), outDir)}`,
);
