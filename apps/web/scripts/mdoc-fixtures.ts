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
 *   - `go_json`, `go_output` only where `json` holds a `doc:<id>` mention:
 *                   the document `mdoc.Parse` must read instead (each such
 *                   mention a `pageLink` to `<id>`, pp-hub's migration of
 *                   document mentions to page links) and its markdown;
 *                   `json` and `output` stay slash-editor's own
 *
 * A case given as a document (`doc`) instead of markdown has `json` = the
 * document and `input` = `output`: markdown slash-editor writes but its own
 * parser reads wrongly (a paragraph starting with a mention), which the Go
 * side must read back as written.
 *
 * The kit's page store knows the titles in `PAGE_TITLES` (written to
 * `testdata/page_titles.json`, which the Go tests render with), every other
 * page reads "Untitled"; hrefs are `/edit/<id>`.
 *
 * Comment marks are persisted by the Go side only (slash-editor's serializer
 * drops them), so their cases go to `testdata/comments/<name>.json` with
 * `json` (the document), `js_output` (`serializeMarkdown(json)`, i.e. the
 * markdown without the comment markers) and `yupdate_b64` (as above, holding
 * y-tiptap's `comment--<hash>` text attributes).
 *
 * Plus `schema.json` (node/mark attribute defaults, mark rank order) which
 * the Go schema table is checked against, and `internal/mdoc/emoji.json`
 * (every shortcode the kit's emoji node knows → its glyph, first entry
 * wins as in Tiptap's `shortcodeToEmoji`), which `mdoc` embeds to decide
 * whether `:name:` is an emoji or text exactly as slash-editor does.
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
import type { PageStore } from "@slash-editor/core";
import type { EmojiOptions } from "@slash-editor/core/emoji";

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
const subPage = (pageId: string): JSONContent => ({ type: "subPage", attrs: { pageId } });
const pageLink = (pageId: string): JSONContent => ({ type: "pageLink", attrs: { pageId } });
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

// Page ids and the titles the fixture PageStore knows (written to
// testdata/page_titles.json for the Go side). P9 is unknown: "Untitled".
const P1 = "0b8e1f2a-6c3d-4e5f-9a1b-7c2d3e4f5a6b";
const P2 = "1c9f2a3b-7d4e-4f60-8b2c-8d3e4f5a6b7c";
const P3 = "p3";
const P4 = "p4";
const P5 = "p5";
const P6 = "p 6";
const P9 = "9e9e9e9e-0000-4000-8000-000000000009";
const PAGE_TITLES: Record<string, string> = {
  [P1]: "Roadmap",
  [P2]: "a*b_[c] <x> & `y`",
  [P3]: "  Spaced out  ",
  [P4]: "   ",
  [P5]: "C:\\path\\[1]",
  [P6]: "Q3 (draft)",
};
/** A subPage block as slash-editor writes it, with `title` in its link line. */
const sp = (id: string, title: string) =>
  `<!-- slash:subPage {"pageId":"${id}"} -->\n[${title}](/edit/${id})`;
/** A pageLink as slash-editor writes it, with `title` in its link. */
const pl = (id: string, title: string) =>
  `<!-- slash:pageLink {"pageId":"${id}"} -->[${title}](/edit/${id})<!-- /slash:pageLink -->`;

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
  // Relative references: a file beside the document, kept as written
  {
    name: "relative-media",
    input:
      '![a](images/a.png)\n\n![b](<../shared/Bùi đẹp.png>)\n\n<!-- slash:file {"size":3} -->\n[spec.pdf](./spec%20v2.pdf)\n\n<!-- slash:video {"poster":"poster.png"} -->\n[clip.mp4](media/clip.mp4)\n',
  },
  { name: "relative-links", input: "See [the spec](../Spec.md#scope) and [data](data/x.csv).\n" },
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
  // Emoji: `:shortcode:` (GitHub's form). A shortcode the dataset does not
  // know stays text, but the tokenizer still consumes it — `x:https://…` is
  // no longer a bare URL because `:https:` is read first.
  { name: "emoji", input: "Ship it :rocket: today :tada:\n" },
  { name: "emoji-alias", input: "Nice :+1: and :thumbsup: and :smile:\n" },
  { name: "emoji-unknown", input: "Meet at 12:30:45 or :not_an_emoji_xyz: then\n" },
  { name: "emoji-uppercase", input: "Loud :SMILE: and :Smile:\n" },
  { name: "emoji-intraword", input: "a:smile:b and x:smile:\n" },
  { name: "emoji-adjacent", input: ":smile::smile: :smile: :smile\n" },
  { name: "emoji-marks", input: "**bold :fire:** and *it :fire:* and ~~:fire:~~\n" },
  { name: "emoji-code", input: "Code `:smile:` stays\n\n```\n:smile:\n```\n" },
  { name: "emoji-escaped", input: "Not \\:smile: and :smile\\: here\n" },
  { name: "emoji-link", input: "[go :rocket: now](https://x.y) and https://a.example/:smile:\n" },
  { name: "emoji-bare-url", input: "See x:https://e.x/y and :smile:https://e.x/z\n" },
  { name: "emoji-email", input: "Mail x:a@b.io or :smile:a@b.io\n" },
  { name: "emoji-heading", input: "## Launch :rocket:\n" },
  {
    name: "emoji-blocks",
    input:
      "- item :one:\n- [ ] task :two:\n\n> quote :three:\n\n| a | b |\n| --- | --- |\n| :four: | x |\n\n> [!NOTE]\n> callout :five:\n",
  },
  { name: "emoji-glyph", input: "Glyph 🎉 stays text\n" },
  { name: "emoji-hard-break", input: "One :smile:  \nnext\n" },
  { name: "emoji-line-start", input: ":wave: hello\n" },
  {
    name: "emoji-mention",
    input:
      'Hi <!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention -->:smile:\n',
  },
  {
    name: "emoji-doc",
    doc: doc(p(text("x"), { type: "emoji", attrs: { name: "smile" } }, text(" y"))),
  },
  { name: "emoji-unknown-bold", input: "**at 12:30:45 :nope_zz: ok** and *:smile:*\n" },
  { name: "emoji-url-before", input: "www.a.example:smile: and https://a.example/x:fire:y z\n" },
  {
    name: "emoji-toggle-columns",
    input:
      "<details>\n<summary>Sum :smile:</summary>\n\nBody :fire:\n\n</details>\n\n<!-- slash:columns -->\n\nLeft :one:\n\n<!-- slash:column -->\n\nRight :two:\n\n<!-- /slash:columns -->\n",
  },
  // Marks never reach an emoji (the parser makes it an atom, and the
  // serializer closes marks around it), so its neighbours keep theirs.
  {
    name: "emoji-between-marks",
    doc: doc(
      p(
        text("a", bold),
        { type: "emoji", attrs: { name: "fire" } },
        text("b", bold),
        text(" "),
        text("go", link("https://x.y")),
        { type: "emoji", attrs: { name: "fire" } },
        text("on", link("https://x.y")),
      ),
    ),
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
  // A definition in the middle of the source: goldmark still lists it last,
  // after blocks that start later (once a slice-bounds panic in Parse).
  {
    name: "lossy-footnote-before-list",
    input: "#### Heading [^6]\n[^6]: Website (mwg.vn) - history\n- A list item\n\nAfter.\n",
    lossy: true,
  },
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
  // Pages. Titles come from PAGE_TITLES (the markdown's own title is not
  // read back); P9 is a page the store does not know.
  { name: "page-sub", input: `${sp(P1, "Roadmap")}\n` },
  { name: "page-sub-stale-title", input: `${sp(P1, "Old name")}\n` },
  { name: "page-sub-escaped-title", input: `${sp(P2, "x")}\n` },
  { name: "page-sub-trimmed-title", input: `${sp(P3, "x")}\n` },
  { name: "page-sub-empty-title", input: `${sp(P4, "x")}\n` },
  { name: "page-sub-unknown", input: `${sp(P9, "Gone")}\n` },
  { name: "page-sub-backslash-title", input: `${sp(P5, "x")}\n` },
  {
    name: "page-sub-href-space",
    input: `${sp(P6, "x").replace(`(/edit/${P6})`, `(</edit/${P6}>)`)}\n`,
  },
  { name: "page-sub-consecutive", input: `${sp(P1, "a")}\n\n${sp(P2, "b")}\n\n${sp(P9, "c")}\n` },
  // A subPage needs a blank line (or the end) after its link line;
  // otherwise the marker is a stray marker (lossy) and the link plain text.
  { name: "page-sub-adjacent-lines", input: `${sp(P1, "a")}\n${sp(P2, "b")}\n`, lossy: true },
  { name: "page-sub-between", input: `before\n${sp(P1, "a")}\nafter\n`, lossy: true },
  { name: "page-sub-blank-lines", input: `one\n\n\n${sp(P1, "a")}\n\n\n\ntwo\n` },
  {
    name: "page-sub-indented",
    input: `   <!-- slash:subPage {"pageId":"${P1}"} -->\n   [a](/edit/${P1})  \n`,
  },
  { name: "page-sub-heading-after", input: `${sp(P1, "a")}\n# Next\n`, lossy: true },
  {
    name: "page-sub-toggle",
    input: `<details open>\n<summary>Pages</summary>\n\n${sp(P1, "a")}\n\n${sp(P2, "b")}\n\n</details>\n`,
  },
  {
    name: "page-sub-columns",
    input: `<!-- slash:columns -->\n\n${sp(P1, "a")}\n\n<!-- slash:column -->\n\ntext\n\n${sp(P2, "b")}\n\n<!-- /slash:columns -->\n`,
  },
  {
    name: "page-sub-callout",
    input: `> [!NOTE]\n> ${sp(P1, "a").replace("\n", "\n> ")}\n`,
  },
  {
    name: "page-sub-quote",
    input: `> quoted\n>\n> ${sp(P1, "a").replace("\n", "\n> ")}\n`,
  },
  {
    name: "page-sub-list-item",
    input: `- item\n\n  ${sp(P1, "a").replace("\n", "\n  ")}\n- next\n`,
  },
  {
    name: "page-sub-tight-list-item",
    input: `- item\n  ${sp(P1, "a").replace("\n", "\n  ")}\n`,
  },
  {
    name: "page-sub-ordered-item",
    input: `1. item\n\n   ${sp(P1, "a").replace("\n", "\n   ")}\n`,
  },
  {
    name: "page-sub-task-item",
    input: `- [ ] task\n  ${sp(P1, "a").replace("\n", "\n  ")}\n`,
  },
  {
    name: "page-sub-doc-nested",
    doc: doc(
      {
        type: "bulletList",
        content: [{ type: "listItem", content: [p(text("item")), subPage(P1)] }],
      },
      { type: "blockquote", content: [subPage(P2)] },
      { type: "callout", attrs: { icon: "⚠️" }, content: [subPage(P1)] },
      {
        type: "details",
        attrs: { open: false },
        content: [
          { type: "detailsSummary", content: [text("t")] },
          { type: "detailsContent", content: [subPage(P9)] },
        ],
      },
      {
        type: "columns",
        content: [
          { type: "column", content: [subPage(P1)] },
          { type: "column", content: [subPage(P2)] },
        ],
      },
      subPage(P3),
    ),
  },
  // Malformed sub-pages: the marker alone is a stray marker (lossy), the
  // link line after it plain text.
  {
    name: "page-sub-no-link",
    input: `<!-- slash:subPage {"pageId":"${P1}"} -->\ntext\n`,
    lossy: true,
  },
  {
    name: "page-sub-no-link-eof",
    input: `<!-- slash:subPage {"pageId":"${P1}"} -->\n`,
    lossy: true,
  },
  {
    name: "page-sub-no-page-id",
    input: `<!-- slash:subPage {} -->\n[a](/edit/${P1})\n`,
    lossy: true,
  },
  {
    name: "page-sub-empty-page-id",
    input: `<!-- slash:subPage {"pageId":""} -->\n[a](/edit/)\n`,
    lossy: true,
  },
  {
    name: "page-sub-numeric-page-id",
    input: `<!-- slash:subPage {"pageId":7} -->\n[a](/edit/7)\n`,
    lossy: true,
  },
  { name: "page-sub-no-payload", input: `<!-- slash:subPage -->\n[a](/edit/${P1})\n`, lossy: true },
  {
    name: "page-sub-bad-payload",
    input: `<!-- slash:subPage {"pageId":} -->\n[a](/edit/${P1})\n`,
    lossy: true,
  },
  {
    name: "page-sub-image-line",
    input: `<!-- slash:subPage {"pageId":"${P1}"} -->\n![a](/edit/${P1})\n`,
    lossy: true,
  },
  {
    name: "page-sub-trailing-text",
    input: `<!-- slash:subPage {"pageId":"${P1}"} -->\n[a](/edit/${P1}) more\n`,
    lossy: true,
  },
  {
    name: "page-sub-extra-attrs",
    input: `<!-- slash:subPage {"pageId":"${P1}","title":"x"} -->\n[a](/edit/${P1})\n`,
  },

  { name: "page-link", input: `See ${pl(P1, "Roadmap")} now\n` },
  { name: "page-link-stale-title", input: `See ${pl(P1, "Old")}\n` },
  { name: "page-link-escaped-title", input: `See ${pl(P2, "x")} and ${pl(P5, "y")}\n` },
  { name: "page-link-unknown", input: `See ${pl(P9, "Gone")} and ${pl(P4, "x")}.\n` },
  { name: "page-link-adjacent", input: `Both ${pl(P1, "a")}${pl(P2, "b")} here\n` },
  { name: "page-link-start", input: `${pl(P1, "a")} first\n` },
  { name: "page-link-only", input: `${pl(P1, "a")}\n\nnext\n` },
  { name: "page-link-start-doc", doc: doc(p(pageLink(P1), text(" first")), p(text("x"))) },
  { name: "page-link-only-doc", doc: doc(p(pageLink(P1)), p(text("next"))) },
  { name: "page-link-bold", input: `Hey **bold ${pl(P1, "a")} bold** and *it ${pl(P2, "b")}*\n` },
  // slash-editor's parser drops the space between a mark and a page link
  // (its marker regex allows leading spaces); Parse keeps what Render wrote.
  {
    name: "page-link-after-mark-doc",
    doc: doc(p(text("b", bold), text(" "), pageLink(P1), text(" c"))),
  },
  { name: "page-link-in-link", input: `Go [to ${pl(P1, "a")} now](https://x.y) ok\n` },
  {
    name: "page-link-marks-doc",
    doc: doc(
      p(
        text("a", bold),
        pageLink(P1),
        text("b", bold),
        text(" "),
        text("go", link("https://x.y")),
        pageLink(P2),
        text("on", link("https://x.y")),
      ),
    ),
  },
  {
    name: "page-link-emoji-mention",
    input: `x :tada:${pl(P1, "a")}<!-- slash:mention {"id":"user:1","label":"A"} -->@A<!-- /slash:mention -->${pl(P2, "b")}:fire: y\n`,
  },
  { name: "page-link-heading", input: `# Title ${pl(P1, "a")}\n` },
  { name: "page-link-hard-break", input: `one  \n${pl(P1, "a")} two\n` },
  { name: "page-link-soft-break", input: `one\n${pl(P1, "a")} two\n` },
  { name: "page-link-blockquote", input: `> cc ${pl(P1, "a")}\n` },
  {
    name: "page-link-list",
    input: `- see ${pl(P1, "a")}\n- [ ] and ${pl(P2, "b")} too\n\n1. then ${pl(P9, "c")}\n`,
  },
  {
    name: "page-link-list-start-doc",
    doc: doc(
      {
        type: "bulletList",
        content: [
          { type: "listItem", content: [p(pageLink(P1))] },
          { type: "listItem", content: [p(pageLink(P2), text(" owns it"))] },
        ],
      },
      {
        type: "taskList",
        content: [{ type: "taskItem", attrs: { checked: true }, content: [p(pageLink(P1))] }],
      },
    ),
  },
  {
    name: "page-link-table",
    input: `| who | what |\n| --- | --- |\n| ${pl(P1, "a")} | see ${pl(P2, "b")} |\n`,
  },
  {
    name: "page-link-table-start-doc",
    doc: doc(
      table(
        [cell("tableHeader", text("page")), cell("tableHeader", text("note"))],
        [cell("tableCell", pageLink(P1)), cell("tableCell", pageLink(P2), text(" v2"))],
      ),
    ),
  },
  { name: "page-link-callout", input: `> [!TIP]\n> Read ${pl(P1, "a")} first.\n` },
  {
    name: "page-link-toggle-summary",
    input: `<details>\n<summary>See ${pl(P1, "a")}</summary>\n\nbody\n\n</details>\n`,
  },
  // Malformed page links: the markers are swallowed, the link between them
  // is ordinary text.
  {
    name: "page-link-unclosed",
    input: `See <!-- slash:pageLink {"pageId":"${P1}"} -->[a](/edit/${P1}) now\n`,
  },
  {
    name: "page-link-no-page-id",
    input: `See <!-- slash:pageLink {} -->[a](/edit/${P1})<!-- /slash:pageLink --> now\n`,
  },
  {
    name: "page-link-no-payload",
    input: `See <!-- slash:pageLink -->[a](/edit/${P1})<!-- /slash:pageLink --> now\n`,
  },
  {
    name: "page-link-bad-payload",
    input: `See <!-- slash:pageLink {"pageId":} -->[a](/edit/${P1})<!-- /slash:pageLink --> now\n`,
  },
  {
    name: "page-link-numeric-page-id",
    input: `See <!-- slash:pageLink {"pageId":7} -->[a](/edit/7)<!-- /slash:pageLink --> now\n`,
  },
  { name: "page-link-close-only", input: `See [a](/edit/${P1})<!-- /slash:pageLink --> now\n` },
  {
    name: "page-link-extra-attrs",
    input: `See <!-- slash:pageLink {"pageId":"${P1}","x":1} -->[a](/edit/${P1})<!-- /slash:pageLink -->\n`,
  },
  {
    name: "page-link-any-content",
    input: `See <!-- slash:pageLink {"pageId":"${P1}"} -->whatever *here*<!-- /slash:pageLink --> ok\n`,
  },
  {
    name: "page-mixed",
    input: `# Plan\n\n${sp(P1, "a")}\n\nSee ${pl(P1, "a")} and <!-- slash:mention {"id":"doc:${P2}","label":"Old"} -->@Old<!-- /slash:mention -->.\n\n${sp(P2, "b")}\n`,
  },
  { name: "mention-doc-start-doc", doc: doc(p(mention(`doc:${P1}`, "Roadmap"), text(" is next"))) },
  {
    name: "mention-doc-empty-id",
    input: `See <!-- slash:mention {"id":"doc:","label":"X"} -->@X<!-- /slash:mention -->\n`,
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
    name: "emoji",
    doc: doc(
      p({ type: "emoji", attrs: { name: "tada" } }, text(" shipped", comment("t1")), text(" ok")),
      p(
        text("we ", comment("t2")),
        { type: "emoji", attrs: { name: "fire" } },
        text(" did", comment("t2")),
        { type: "emoji", attrs: { name: "rocket" } },
      ),
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
  {
    name: "page-link",
    doc: doc(
      p(
        text("see "),
        text("this", comment("t1")),
        pageLink(P1),
        text(" and", comment("t1")),
        text(" more"),
      ),
      p(pageLink(P2), text(" first", comment("t2"))),
      p(text("x "), pageLink(P9), text(" y", comment("t3"))),
    ),
  },
  {
    name: "sub-page",
    doc: doc(p(text("above", comment("t1"))), subPage(P1), p(text("below", comment("t1")))),
  },
];

const here = path.dirname(fileURLToPath(import.meta.url));
const outDir = path.resolve(here, "../../server/internal/mdoc/testdata/fixtures");
const commentDir = path.resolve(here, "../../server/internal/mdoc/testdata/comments");
const schemaFile = path.resolve(here, "../../server/internal/mdoc/testdata/schema.json");
const titlesFile = path.resolve(here, "../../server/internal/mdoc/testdata/page_titles.json");
const emojiFile = path.resolve(here, "../../server/internal/mdoc/emoji.json");

/** Knows PAGE_TITLES' pages; nothing else is called while serializing. */
const pageStore: PageStore = {
  create: () => Promise.reject(new Error("fixture page store")),
  update: () => {},
  peek: (id) =>
    Object.hasOwn(PAGE_TITLES, id) ? { id, parentId: null, title: PAGE_TITLES[id] } : undefined,
  load: async () => undefined,
  listChildren: () => [],
  search: () => [],
  subscribe: () => () => {},
};

// resolveHref stays createMdocKit's default, `/edit/<id>`.
const kit = createMdocKit({
  pages: { store: pageStore, currentPageId: null, onNavigate: () => {} },
});
const schema = getSchema(kit);

/**
 * The document `mdoc.Parse` reads where slash-editor's parser reads a
 * `doc:<id>` mention: a pageLink to `<id>` (pp-hub's pre-pages document
 * mentions; the editor writes the pageLink form back on the next edit).
 */
function docMentionsToPageLinks(node: JSONContent): JSONContent {
  const id: unknown = node.attrs?.id;
  if (node.type === "mention" && typeof id === "string" && id.startsWith("doc:") && id.length > 4) {
    return { type: "pageLink", attrs: { pageId: id.slice(4) } };
  }
  return node.content ? { ...node, content: node.content.map(docMentionsToPageLinks) } : node;
}

/**
 * `go_json`/`go_output`, only for a document with `doc:` mentions: what the
 * Go side must parse from `input` and write back, where it deliberately
 * departs from slash-editor's parse (`json`/`output` stay slash-editor's).
 */
function goExpectation(json: JSONContent): { go_json?: JSONContent; go_output?: string } {
  const goJson = docMentionsToPageLinks(json);
  if (JSON.stringify(goJson) === JSON.stringify(json)) return {};
  return { go_json: goJson, go_output: serializeMarkdown(goJson, kit) };
}

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
      ...goExpectation(json),
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
    ...goExpectation(json),
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

writeFileSync(titlesFile, `${JSON.stringify(PAGE_TITLES, null, 2)}\n`);

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

const emojiNode = kit.find((extension) => extension.name === "emoji");
if (!emojiNode) throw new Error("createMdocKit() has no emoji node");
const glyphs = new Map<string, string>();
for (const item of (emojiNode.options as EmojiOptions).emojis) {
  if (!item.emoji) continue;
  for (const code of [item.name, ...item.shortcodes]) {
    if (!glyphs.has(code)) glyphs.set(code, item.emoji);
  }
}
writeFileSync(emojiFile, `${JSON.stringify(Object.fromEntries(glyphs), null, 0)}\n`);
console.log(`wrote ${glyphs.size} emoji shortcodes to ${path.relative(process.cwd(), emojiFile)}`);
