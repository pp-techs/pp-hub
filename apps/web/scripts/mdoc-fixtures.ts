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
 * A case given as a document (`doc`) instead of markdown has `json` = the
 * document and `input` = `output`: markdown slash-editor writes but its own
 * parser reads wrongly (a paragraph starting with a mention), which the Go
 * side must read back as written.
 *
 * Comment marks are persisted by the Go side only (slash-editor's serializer
 * drops them), so their cases go to `testdata/comments/<name>.json` with
 * `json` (the document), `js_output` (`serializeMarkdown(json)`, i.e. the
 * markdown without the comment markers) and `yupdate_b64` (as above, holding
 * y-tiptap's `comment--<hash>` text attributes).
 *
 * Plus `schema.json` (node/mark attribute defaults, mark rank order) which
 * the Go schema table is checked against.
 *
 * The kit is `createMdocKit()` from `src/lib/collab/mdoc-kit.ts`, the same
 * v1 schema the editor mounts. Output is deterministic: rerunning it on the
 * same `@slash-editor/core` version changes nothing (Y client ids are pinned).
 */
import { mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { getSchema, type JSONContent } from "@tiptap/core";
import { parseMarkdown, serializeMarkdown } from "@slash-editor/core/markdown";
import { prosemirrorJSONToYXmlFragment } from "@tiptap/y-tiptap";
import * as Y from "yjs";
import { createMdocKit } from "../src/lib/collab/mdoc-kit.ts";

type Case = { name: string; lossy?: boolean } & (
  | { input: string; frontmatter?: string }
  | { doc: JSONContent }
);

type Mark = NonNullable<JSONContent["marks"]>[number];

const doc = (...content: JSONContent[]): JSONContent => ({ type: "doc", content });
const p = (...content: JSONContent[]): JSONContent => ({ type: "paragraph", content });
const text = (value: string, ...marks: Mark[]): JSONContent =>
  marks.length ? { type: "text", text: value, marks } : { type: "text", text: value };
const mention = (id: string | null, label: string): JSONContent => ({
  type: "mention",
  attrs: { id, label },
});
const hardBreak: JSONContent = { type: "hardBreak" };
const bold: Mark = { type: "bold" };
const italic: Mark = { type: "italic" };
const code: Mark = { type: "code" };
const link = (href: string): Mark => ({ type: "link", attrs: { href } });
const comment = (threadId: string): Mark => ({ type: "comment", attrs: { threadId } });
const cell = (type: "tableHeader" | "tableCell", ...content: JSONContent[]): JSONContent => ({
  type,
  content: [p(...content)],
});
const table = (...rows: JSONContent[][]): JSONContent => ({
  type: "table",
  content: rows.map((content) => ({ type: "tableRow", content })),
});

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
  // Mentions: `<!-- slash:mention {id,label} -->@label<!-- /slash:mention -->`
  {
    name: "mention-user",
    input:
      'Ping <!-- slash:mention {"id":"user:3f1c2a44-9b0e-4c1d-8a7e-2b6f0c9d1e55","label":"Lan Anh"} -->@Lan Anh<!-- /slash:mention --> today.\n',
  },
  {
    name: "mention-doc",
    input:
      'See <!-- slash:mention {"id":"doc:0b8e1f2a-6c3d-4e5f-9a1b-7c2d3e4f5a6b","label":"Runbook"} -->@Runbook<!-- /slash:mention -->\n',
  },
  {
    name: "mention-no-payload",
    input: "Hi <!-- slash:mention -->@Bob<!-- /slash:mention --> there\n",
  },
  {
    name: "mention-label-only",
    input: 'Hi <!-- slash:mention {"label":"Bob"} -->@Robert<!-- /slash:mention -->\n',
  },
  {
    name: "mention-id-only",
    input: 'Hi <!-- slash:mention {"id":"user:1"} -->@<!-- /slash:mention --> there\n',
  },
  {
    name: "mention-specials",
    input:
      'Hi <!-- slash:mention {"id":"user:2","label":"a*b_c [x] <y> & \\u002d\\u002d z"} -->@a*b_c [x] <y> & -- z<!-- /slash:mention -->!\n',
  },
  {
    name: "mention-unicode",
    input:
      'Chào <!-- slash:mention {"id":"user:3","label":"Nguyễn Văn 🎉"} -->@Nguyễn Văn 🎉<!-- /slash:mention -->.\n',
  },
  {
    name: "mention-adjacent",
    input:
      'Both <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention --><!-- slash:mention {"id":"user:2","label":"B"} -->@B<!-- /slash:mention --> here\n',
  },
  {
    name: "mention-marks",
    input:
      'Hey **bold <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention --> bold** and *it <!-- slash:mention {"id":"user:2","label":"B"} -->@B<!-- /slash:mention -->*\n',
  },
  {
    name: "mention-link",
    input:
      'Go [to <!-- slash:mention {"id":"doc:1","label":"D"} -->@D<!-- /slash:mention --> now](https://x.y) ok\n',
  },
  {
    name: "mention-hard-break",
    input:
      'One <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention -->  \nnext line\n',
  },
  {
    name: "mention-heading",
    input: '## Owner <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention -->\n',
  },
  {
    name: "mention-list",
    input:
      '- ask <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention -->\n- [ ] and <!-- slash:mention {"id":"user:2","label":"B"} -->@B<!-- /slash:mention --> too\n\n1. then <!-- slash:mention {"id":"doc:9","label":"Doc"} -->@Doc<!-- /slash:mention -->\n',
  },
  {
    name: "mention-nested-list",
    input:
      '- top\n  - by <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention --> deep\n',
  },
  {
    name: "mention-table",
    input:
      '| who | what |\n| --- | --- |\n| by <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention --> | see <!-- slash:mention {"id":"doc:2","label":"Spec"} -->@Spec<!-- /slash:mention --> |\n',
  },
  {
    name: "mention-callout",
    input:
      '> [!NOTE]\n> Ask <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention --> first.\n',
  },
  {
    name: "mention-blockquote",
    input: '> cc <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention -->\n',
  },
  {
    name: "mention-details",
    input:
      '<details>\n<summary>By <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention --></summary>\n\nBody <!-- slash:mention {"id":"user:2","label":"B"} -->@B<!-- /slash:mention -->.\n\n</details>\n',
  },
  {
    name: "mention-no-at",
    input: 'Hi <!-- slash:mention {"id":"user:1","label":"A"} -->A<!-- /slash:mention --> x\n',
  },
  {
    name: "mention-unclosed",
    input: 'Hi <!-- slash:mention {"id":"user:1","label":"A"} -->@A and more\n',
  },
  {
    name: "mention-bad-payload",
    input: 'Hi <!-- slash:mention {"id":} -->@A<!-- /slash:mention --> x\n',
  },
  // Videos: marker (poster) + one link line
  {
    name: "video",
    input: "<!-- slash:video -->\n[attachment:0123abcd](attachment:0123abcd)\n",
  },
  {
    name: "video-poster",
    input:
      '<!-- slash:video {"poster":"attachment:beef03"} -->\n[attachment:0123abcd](attachment:0123abcd)\n',
  },
  {
    name: "video-label",
    input: "<!-- slash:video -->\n[clip.mp4](attachment:0123abcd)\n",
  },
  {
    name: "video-external",
    input: "<!-- slash:video -->\n[https://cdn.example/a b.mp4](<https://cdn.example/a b.mp4>)\n",
  },
  { name: "video-no-marker", input: "[clip](attachment:0123abcd)\n" },
  {
    name: "video-between",
    input: "Before.\n\n<!-- slash:video -->\n[attachment:aa](attachment:aa)\n\nAfter.\n",
  },
  // Embeds (bookmarks): marker (mode, description, thumbnail) + [title](url)
  {
    name: "embed-bookmark",
    input: '<!-- slash:embed {"mode":"bookmark"} -->\n[https://example.com](https://example.com)\n',
  },
  {
    name: "embed-bookmark-full",
    input:
      '<!-- slash:embed {"mode":"bookmark","description":"An example \\u002d\\u002d site","thumbnail":"attachment:c0ffee"} -->\n[Example \\[site\\]](https://example.com/a?b=1)\n',
  },
  {
    name: "embed-bookmark-title",
    input: '<!-- slash:embed {"mode":"bookmark"} -->\n[Example](https://example.com)\n',
  },
  {
    name: "embed-iframe",
    input: '<!-- slash:embed {"mode":"iframe"} -->\n[Map](<https://maps.example/embed?q=(1,2)>)\n',
  },
  // Media inside containers
  {
    name: "media-in-list",
    input:
      '- item\n\n  <!-- slash:video -->\n  [attachment:aa](attachment:aa)\n- next\n\n  <!-- slash:embed {"mode":"bookmark"} -->\n  [X](https://x.example)\n',
  },
  {
    name: "media-in-callout",
    input:
      '> [!TIP]\n> Watch:\n>\n> <!-- slash:video {"poster":"attachment:bb"} -->\n> [attachment:aa](attachment:aa)\n>\n> <!-- slash:embed {"mode":"bookmark","description":"d"} -->\n> [X](https://x.example)\n',
  },
  {
    name: "media-in-columns",
    input:
      '<!-- slash:columns -->\n\n<!-- slash:video -->\n[attachment:aa](attachment:aa)\n\n<!-- slash:column -->\n\n<!-- slash:embed {"mode":"bookmark"} -->\n[X](https://x.example)\n\n<!-- /slash:columns -->\n',
  },
  {
    name: "media-in-details",
    input:
      '<details open>\n<summary>Media</summary>\n\n<!-- slash:video -->\n[attachment:aa](attachment:aa)\n\n<!-- slash:embed {"mode":"bookmark"} -->\n[X](https://x.example)\n\n</details>\n',
  },
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
  // Written by slash-editor, misread by its own parser: a line that starts
  // with a mention is an HTML block to marked (or swallowed whole as a stray
  // marker when it also ends with one).
  { name: "mention-paragraph-only", doc: doc(p(mention("user:1", "Ann"))) },
  {
    name: "mention-paragraph-start",
    doc: doc(p(mention("user:1", "Ann"), text(" said **hi**"))),
  },
  {
    name: "mention-paragraph-start-marks",
    doc: doc(
      p(mention("doc:7", "Spec"), text(" is "), text("final", bold), text(".")),
      p(text("next")),
    ),
  },
  {
    name: "mention-after-hard-break",
    doc: doc(p(text("one"), hardBreak, mention("user:2", "Bo"), text(" two"))),
  },
  {
    name: "mention-after-soft-break",
    doc: doc(p(text("one\n"), mention("user:2", "Bo"), hardBreak, mention("user:3", "Cy"))),
  },
  { name: "mention-after-one-char", doc: doc(p(text("x "), mention("user:1", "Ann"))) },
  { name: "mention-no-id-start", doc: doc(p(mention(null, "guest"), text(" joined"))) },
  {
    name: "mention-list-start",
    doc: doc(
      {
        type: "bulletList",
        content: [
          { type: "listItem", content: [p(mention("user:1", "Ann"))] },
          { type: "listItem", content: [p(mention("user:2", "Bo"), text(" owns it"))] },
        ],
      },
      {
        type: "taskList",
        content: [
          { type: "taskItem", attrs: { checked: true }, content: [p(mention("user:3", "Cy"))] },
        ],
      },
    ),
  },
  {
    name: "mention-quote-start",
    doc: doc(
      { type: "blockquote", content: [p(mention("user:1", "Ann"), text(" wrote"))] },
      { type: "callout", attrs: { icon: "⚠️" }, content: [p(mention("user:2", "Bo"))] },
    ),
  },
  {
    name: "mention-table-start",
    doc: doc(
      table(
        [cell("tableHeader", text("who")), cell("tableHeader", text("note"))],
        [
          cell("tableCell", mention("user:1", "Ann")),
          cell("tableCell", mention("doc:2", "Plan"), text(" v2")),
        ],
      ),
    ),
  },
  {
    name: "mention-heading-start",
    doc: doc({ type: "heading", attrs: { level: 3 }, content: [mention("user:1", "Ann")] }),
  },
  {
    name: "mention-columns-start",
    doc: doc({
      type: "columns",
      content: [
        { type: "column", content: [p(mention("user:1", "Ann"))] },
        { type: "column", content: [p(mention("user:2", "Bo"), text(" too"))] },
      ],
    }),
  },
  {
    name: "mention-details-start",
    doc: doc({
      type: "details",
      attrs: { open: true },
      content: [
        { type: "detailsSummary", content: [text("Owners")] },
        { type: "detailsContent", content: [p(mention("user:1", "Ann"))] },
      ],
    }),
  },
];

/** Comment-mark documents: Go-only persistence (see the file comment). */
const commentCases: { name: string; doc: JSONContent }[] = [
  { name: "simple", doc: doc(p(text("Hello "), text("world", comment("t1")), text("!"))) },
  { name: "paragraph-start", doc: doc(p(text("Hello", comment("t1")), text(" world"))) },
  { name: "whole-paragraph", doc: doc(p(text("Hello world", comment("t1")))) },
  {
    name: "overlap",
    doc: doc(
      p(
        text("a "),
        text("b ", comment("t1")),
        text("c ", comment("t1"), comment("t2")),
        text("d", comment("t2")),
        text(" e"),
      ),
    ),
  },
  {
    name: "nested",
    doc: doc(
      p(
        text("x "),
        text("outer ", comment("t1")),
        text("inner", comment("t1"), comment("t2")),
        text(" outer", comment("t1")),
      ),
    ),
  },
  {
    name: "same-range",
    doc: doc(p(text("say "), text("both", comment("t1"), comment("t2")), text(" here"))),
  },
  {
    name: "across-marks",
    doc: doc(
      p(
        text("plain "),
        text("bold", bold, comment("t1")),
        text(" and ", comment("t1")),
        text("italic", italic, comment("t1")),
        text(" end"),
      ),
    ),
  },
  {
    name: "inside-mark",
    doc: doc(
      p(
        text("x "),
        text("ab", bold),
        text("cd", bold, comment("t1")),
        text("ef", bold),
        text(" y"),
      ),
    ),
  },
  {
    name: "mark-boundary",
    doc: doc(
      p(
        text("x "),
        text("ab", bold),
        text("cd", bold, comment("t1")),
        text("ef", comment("t1")),
        text(" y"),
      ),
    ),
  },
  {
    // code excludes every other mark: a thread around inline code is two
    // ranges, one on each side.
    name: "code",
    doc: doc(
      p(
        text("run ", comment("t1")),
        text("make all", code),
        text(" now", comment("t1")),
        text(" "),
        text("x", code),
        text("y", comment("t2")),
      ),
    ),
  },
  {
    name: "link",
    doc: doc(
      p(
        text("see "),
        text("the ", link("https://x.example")),
        text("docs", link("https://x.example"), comment("t1")),
        text(" now"),
      ),
    ),
  },
  {
    name: "hard-break",
    doc: doc(
      p(text("one", comment("t1")), hardBreak, text("two", comment("t1")), text(" x")),
      p(text("one"), hardBreak, text("two", comment("t2")), text(" y")),
      p(text("one", comment("t3")), hardBreak, text("two")),
    ),
  },
  {
    name: "soft-break",
    doc: doc(p(text("line one\n"), text("line two", comment("t1")), text(" end"))),
  },
  {
    name: "mention",
    doc: doc(
      p(mention("user:1", "Ann"), text(" said", comment("t1")), text(" hi")),
      p(text("ask ", comment("t2")), mention("user:2", "Bo"), text(" now", comment("t2"))),
    ),
  },
  {
    name: "blocks",
    doc: doc(
      { type: "heading", attrs: { level: 2 }, content: [text("Title", comment("h1"))] },
      {
        type: "bulletList",
        content: [{ type: "listItem", content: [p(text("item", comment("l1")), text(" rest"))] }],
      },
      {
        type: "taskList",
        content: [
          {
            type: "taskItem",
            attrs: { checked: false },
            content: [p(text("todo", comment("l2")))],
          },
        ],
      },
      { type: "blockquote", content: [p(text("quoted", comment("q1")))] },
      {
        type: "callout",
        attrs: { icon: "💡" },
        content: [p(text("tip", comment("q2")), text(" here"))],
      },
      table(
        [cell("tableHeader", text("head", comment("c1"))), cell("tableHeader", text("h2"))],
        [
          cell("tableCell", text("cell", comment("c2"))),
          cell("tableCell", text("x "), text("y", comment("c3"))),
        ],
      ),
      {
        type: "details",
        attrs: { open: false },
        content: [
          { type: "detailsSummary", content: [text("Sum", comment("d1")), text("mary")] },
          { type: "detailsContent", content: [p(text("body", comment("d2")))] },
        ],
      },
    ),
  },
  {
    name: "multi-paragraph",
    doc: doc(
      p(text("first "), text("end", comment("t1"))),
      p(text("start", comment("t1")), text(" second")),
    ),
  },
  {
    name: "same-thread-twice",
    doc: doc(p(text("a", comment("t1")), text(" b "), text("c", comment("t1")))),
  },
  {
    name: "whitespace",
    doc: doc(
      p(text("a"), text(" b ", comment("t1")), text("c"), text(" ", comment("t2")), text("d")),
    ),
  },
  {
    name: "astral",
    doc: doc(p(text("🎉 party", comment("t1")), text(" 🎉"), text("x🎉", comment("t2")))),
  },
  {
    name: "special-thread-ids",
    doc: doc(
      p(
        text("a "),
        text("b", comment('t--1"x<y>')),
        text(" "),
        text("c", comment("0b8e1f2a-6c3d-4e5f-9a1b-7c2d3e4f5a6b")),
      ),
    ),
  },
  {
    name: "escaped-text",
    doc: doc(p(text("*not* "), text("<b> & [x]", comment("t1")), text(" _y_"))),
  },
];

const here = path.dirname(fileURLToPath(import.meta.url));
const outDir = path.resolve(here, "../../server/internal/mdoc/testdata/fixtures");
const commentDir = path.resolve(here, "../../server/internal/mdoc/testdata/comments");
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

function write(dir: string, name: string, fixture: object) {
  writeFileSync(path.join(dir, `${name}.json`), `${JSON.stringify(fixture, null, 2)}\n`);
}

rmSync(outDir, { recursive: true, force: true });
mkdirSync(outDir, { recursive: true });
const seen = new Set<string>();
for (const fixtureCase of cases) {
  const { name, lossy = false } = fixtureCase;
  if (seen.has(name)) throw new Error(`duplicate fixture name ${name}`);
  seen.add(name);
  if ("doc" in fixtureCase) {
    const json = fixtureCase.doc;
    const output = serializeMarkdown(json, kit);
    write(outDir, name, {
      name,
      input: output,
      frontmatter: "",
      json,
      output,
      yupdate_b64: encodeUpdate(json),
      expect_lossy: lossy,
    });
    continue;
  }
  const { input, frontmatter = "" } = fixtureCase;
  const json = parseMarkdown(input, kit);
  write(outDir, name, {
    name,
    input: frontmatter + input,
    frontmatter,
    json,
    output: serializeMarkdown(json, kit),
    yupdate_b64: encodeUpdate(json),
    expect_lossy: lossy,
  });
}

rmSync(commentDir, { recursive: true, force: true });
mkdirSync(commentDir, { recursive: true });
const seenComments = new Set<string>();
for (const { name, doc: json } of commentCases) {
  if (seenComments.has(name)) throw new Error(`duplicate comment fixture name ${name}`);
  seenComments.add(name);
  write(commentDir, name, {
    name,
    json,
    js_output: serializeMarkdown(json, kit),
    yupdate_b64: encodeUpdate(json),
  });
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
  `wrote ${readdirSync(outDir).length} fixtures to ${path.relative(process.cwd(), outDir)}, ` +
    `${readdirSync(commentDir).length} to ${path.relative(process.cwd(), commentDir)}`,
);
