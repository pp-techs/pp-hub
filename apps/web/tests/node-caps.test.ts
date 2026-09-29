import { expect, test } from "vite-plus/test";
import { commonCaps, canRestructure, capsOf, type NodeCaps } from "../src/lib/api/caps.ts";
import { nodeActions, type NodeActionHandlers } from "../src/components/node-actions.tsx";
import type { Node } from "../src/lib/api/types.ts";

const CAPS: Record<string, NodeCaps> = {
  owner: { view: true, download: true, share: true, edit: true, owner: true },
  editor: { view: true, download: true, share: false, edit: true, owner: false },
  reader: { view: true, download: true, share: false, edit: false, owner: false },
  noDownload: { view: true, download: false, share: false, edit: false, owner: false },
};

const file = (caps?: NodeCaps, extra: Partial<Node> = {}): Node =>
  ({ id: "n1", space_id: "s1", kind: "file", name: "report.txt", caps, ...extra }) as Node;

const handlers = {
  onPreview: () => {},
  onShowDetails: () => {},
  onToggleStar: () => {},
  onRename: () => {},
  onMove: () => {},
  onCopy: () => {},
  onClipboard: () => {},
  onDeleteRequest: () => {},
} satisfies NodeActionHandlers;

const keys = (node: Node, canWriteHere = true) =>
  nodeActions(node, handlers, () => {}, false, canWriteHere).map((a) => a.key);

test("a node the server said nothing about is treated as read-only", () => {
  // The floor matters: guessing "allowed" is how a menu ends up offering
  // actions that 403, which is the whole reason caps travel with the node.
  expect(capsOf(file())).toEqual(CAPS.noDownload);
  expect(capsOf(null).view).toBe(true);
});

test("the menu offers only what the caller holds", () => {
  expect(keys(file(CAPS.owner))).toContain("share");
  // Same folder, neighbouring row: an editor may change it but not pass it on.
  expect(keys(file(CAPS.editor))).not.toContain("share");
  expect(keys(file(CAPS.editor))).toContain("rename");

  const reader = keys(file(CAPS.reader));
  expect(reader).toContain("download");
  expect(reader).toContain("copy"); // download is enough to take a copy
  expect(reader).not.toContain("rename");
  expect(reader).not.toContain("move");
  expect(reader).not.toContain("cut");
  expect(reader).not.toContain("trash");
});

test("without download, nothing that saves the bytes is offered", () => {
  const menu = keys(file(CAPS.noDownload, { mime: "application/pdf" }));
  expect(menu).not.toContain("download");
  expect(menu).not.toContain("copy");
  // Reading is view's, not download's: `/content` takes view, so opening the
  // bytes in a tab stays available and only saving them away is gated.
  expect(menu).toContain("open-tab");
  expect(menu).toContain("preview"); // the derived preview is still theirs
});

test("export with images is a markdown download, so it needs both", () => {
  const doc = (caps: NodeCaps) => file(caps, { name: "notes.md", mime: "text/markdown" });
  expect(keys(doc(CAPS.reader))).toContain("export");
  expect(keys(doc(CAPS.noDownload))).not.toContain("export");
  expect(keys(file(CAPS.owner))).not.toContain("export");
});

test("a copy needs somewhere to land", () => {
  expect(keys(file(CAPS.owner), false)).not.toContain("copy");
  expect(keys(file(CAPS.owner), false)).toContain("clipboard-copy");
});

test("an office file says Open to a reader and Edit to an editor", () => {
  // Action labels are `MessageDescriptor`s so that the static menus defined at
  // module scope are not frozen to whichever catalog loaded first. The English
  // source text travels on the descriptor, which is what a consumer renders
  // through `i18n._()` in the source locale — so that is what is asserted.
  const label = (caps: NodeCaps) =>
    nodeActions(file(caps, { name: "brief.docx" }), handlers, () => {}, false, true).find(
      (a) => a.key === "edit",
    )?.label.message;
  expect(label(CAPS.reader)).toBe("Open");
  expect(label(CAPS.editor)).toBe("Edit");
});

test("a team folder's root is restructured by its managers, not its contributors", () => {
  const team = (caps: NodeCaps) =>
    canRestructure(file(caps, { kind: "dir", team_folder: true, in_team: true }));
  // An editor inside a team folder is a contributor: they may add to it, and
  // may not rename or trash the folder itself.
  expect(team(CAPS.editor)).toBe(false);
  expect(team({ ...CAPS.editor, share: true })).toBe(true);
  // Ordinary folders keep the plain rule.
  expect(canRestructure(file(CAPS.editor, { kind: "dir" }))).toBe(true);
});

test("a bulk action needs the capability on every node in the selection", () => {
  const mixed = commonCaps([file(CAPS.owner), file(CAPS.reader)]);
  expect(mixed.view).toBe(true);
  expect(mixed.edit).toBe(false);
  expect(mixed.share).toBe(false);
  expect(commonCaps([file(CAPS.owner), file(CAPS.owner)]).edit).toBe(true);
  expect(commonCaps([]).edit).toBe(false);
});
