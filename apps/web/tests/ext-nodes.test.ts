import { describe, expect, it } from "vite-plus/test";
import { opensInEditor, sortNodes, type Node } from "../src/lib/api";

const node = (id: string, kind: Node["kind"], extra: Partial<Node> = {}): Node =>
  ({ id, name: id, kind, space_id: "s1", ...extra }) as Node;

describe("extension nodes", () => {
  it("open in the editor whatever their name", () => {
    expect(opensInEditor(node("board", "ext", { ext_kind: "kanban.board" }))).toBe(true);
  });

  it("sort with the files, after folders", () => {
    const sorted = sortNodes(
      [node("b", "ext"), node("z", "dir"), node("a", "file"), node("c", "ext")],
      "name",
      "asc",
    );
    expect(sorted.map((n) => n.id)).toEqual(["z", "a", "b", "c"]);
  });
});
