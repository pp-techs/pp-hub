import { beforeEach, expect, test, vi } from "vite-plus/test";
import { QueryClient } from "@tanstack/react-query";
import * as Y from "yjs";
import { moveNode as moveNodeRequest } from "../src/lib/api/index.ts";
import type * as Api from "../src/lib/api/index.ts";
import { ROOM_META, createPageStore } from "../src/lib/collab/page-store.ts";
import type { PageSummary } from "../src/lib/api/types.ts";
import { keys } from "../src/lib/queries/keys.ts";
import { seedPages } from "../src/lib/queries/pages.ts";

vi.mock("../src/lib/api/index.ts", async (importOriginal) => ({
  ...(await importOriginal<typeof Api>()),
  moveNode: vi.fn(),
}));

const moveNode = vi.mocked(moveNodeRequest);
beforeEach(() => moveNode.mockReset());

const page = (id: string, title: string, extra: Partial<PageSummary> = {}): PageSummary => ({
  id,
  parent_id: null,
  title,
  icon: null,
  cover: null,
  has_children: false,
  trashed: false,
  ...extra,
});

/**
 * `subscribe` coalesces a burst of cache events into one call, queued as a
 * microtask; awaiting resumes after it, so that call has been made.
 */
const settle = () => Promise.resolve();

const setup = () => {
  const queryClient = new QueryClient();
  const store = createPageStore(queryClient, { kind: "node", spaceId: "s1" });
  return { queryClient, store };
};

test("peek returns the same object until that page changes", () => {
  const { queryClient, store } = setup();
  seedPages(queryClient, "node", [page("a", "Alpha"), page("b", "Beta")]);
  const alpha = store.peek("a");
  expect(alpha).toMatchObject({ id: "a", parentId: null, title: "Alpha" });

  // A refetch that changed nothing, another page changing, an unrelated query.
  seedPages(queryClient, "node", [page("a", "Alpha"), page("b", "Beta, renamed")]);
  queryClient.setQueryData(["children", "s1", null], []);
  expect(store.peek("a")).toBe(alpha);
  expect(store.peek("b")?.title).toBe("Beta, renamed");

  seedPages(queryClient, "node", [page("a", "Alpha", { icon: "🚀" })]);
  const changed = store.peek("a");
  expect(changed).not.toBe(alpha);
  expect(changed?.icon).toBe("🚀");
  expect(store.peek("a")).toBe(changed);
});

test("a share link's pages never answer for the signed-in scope", () => {
  const { queryClient, store } = setup();
  seedPages(queryClient, "share:t1", [page("a", "Alpha")]);
  expect(store.peek("a")).toBeUndefined();
});

test("subscribe fires when a page changes, and only then", async () => {
  const { queryClient, store } = setup();
  seedPages(queryClient, "node", [page("a", "Alpha")]);
  const listener = vi.fn();
  const unsubscribe = store.subscribe(listener);

  seedPages(queryClient, "node", [page("a", "Alpha")]);
  await settle();
  // The first event under a fresh subscription has nothing to compare with.
  listener.mockClear();

  seedPages(queryClient, "node", [page("a", "Alpha")]);
  queryClient.setQueryData(["children", "s1", null], []);
  await settle();
  expect(listener).not.toHaveBeenCalled();

  seedPages(queryClient, "node", [page("a", "Alpha, renamed"), page("b", "Beta")]);
  await settle();
  expect(listener).toHaveBeenCalledTimes(1);

  unsubscribe();
  seedPages(queryClient, "node", [page("a", "Alpha, again")]);
  await settle();
  expect(listener).toHaveBeenCalledTimes(1);
});

test("the open page's icon and cover come from the room's meta map", async () => {
  const { queryClient, store } = setup();
  seedPages(queryClient, "node", [page("a", "Alpha", { icon: "📄" }), page("b", "Beta")]);
  const doc = new Y.Doc();
  const meta = doc.getMap(ROOM_META);
  meta.set("icon", "🎯");
  const listener = vi.fn();
  store.subscribe(listener);
  const unbind = store.bindMeta("a", meta);

  const bound = store.peek("a");
  expect(bound).toMatchObject({ title: "Alpha", icon: "🎯" });
  expect(store.peek("a")).toBe(bound);
  // Only the bound page reads the map.
  expect(store.peek("b")?.icon).toBeUndefined();

  await settle();
  listener.mockClear();
  await store.update("a", { icon: "", cover: "attachment:abc" });
  await settle();
  expect(listener).toHaveBeenCalled();
  expect(meta.get("icon")).toBeUndefined();
  expect(meta.get("cover")).toBe("attachment:abc");
  const covered = store.peek("a");
  expect(covered).not.toBe(bound);
  expect(covered?.icon).toBeUndefined();
  expect(store.coverUrl(covered!)).toBe("/api/v1/nodes/a/attachments/abc");

  await expect(store.update("b", { icon: "🎯" })).rejects.toThrow();

  unbind();
  expect(store.peek("a")?.icon).toBe("📄");
});

const children = (queryClient: QueryClient, parentId: string) =>
  queryClient
    .getQueryData<PageSummary[]>(keys.pageChildren("node", parentId))
    ?.map((summary) => summary.id);

/** A parent `p` with sub-pages a, b, c, and `d` under `a`, listed as the tree would have. */
const seedTree = (queryClient: QueryClient) => {
  const pages = [
    page("p", "Parent", { has_children: true }),
    page("a", "Alpha", { parent_id: "p", has_children: true }),
    page("b", "Beta", { parent_id: "p" }),
    page("c", "Gamma", { parent_id: "p" }),
    page("d", "Delta", { parent_id: "a" }),
  ];
  seedPages(queryClient, "node", pages);
  queryClient.setQueryData(keys.pageChildren("node", "p"), pages.slice(1, 4));
  queryClient.setQueryData(keys.pageChildren("node", "a"), pages.slice(4));
  queryClient.setQueryData(keys.pageChildren("node", "b"), []);
};

test("move shows the new order at once and sends the index", async () => {
  const { queryClient, store } = setup();
  seedTree(queryClient);
  // What the tree shows when the request leaves, before the server has answered.
  let atRequest: string[] | undefined;
  moveNode.mockImplementationOnce(async () => {
    atRequest = children(queryClient, "p");
  });

  await store.move("c", { parentId: "p", index: 0 });
  expect(moveNode).toHaveBeenCalledWith("c", "p", 0);
  expect(atRequest).toEqual(["c", "a", "b"]);
  expect(children(queryClient, "p")).toEqual(["c", "a", "b"]);
});

test("move to another parent leaves the old list and joins the new one", async () => {
  const { queryClient, store } = setup();
  seedTree(queryClient);
  moveNode.mockResolvedValueOnce(undefined);

  await store.move("c", { parentId: "b", index: 0 });
  expect(children(queryClient, "p")).toEqual(["a", "b"]);
  expect(children(queryClient, "b")).toEqual(["c"]);
  expect(store.peek("c")?.parentId).toBe("b");
  expect(store.peek("b")).toMatchObject({ hasChildren: true });
});

test("a refused move puts the tree back", async () => {
  const { queryClient, store } = setup();
  seedTree(queryClient);
  moveNode.mockRejectedValueOnce(new Error("no"));

  await expect(store.move("c", { parentId: "b", index: 0 })).rejects.toThrow("no");
  expect(children(queryClient, "p")).toEqual(["a", "b", "c"]);
  expect(children(queryClient, "b")).toEqual([]);
  expect(store.peek("c")?.parentId).toBe("p");
});

test("a page cannot move into itself or its own descendant, nor to the top level", async () => {
  const { queryClient, store } = setup();
  seedTree(queryClient);

  await store.move("a", { parentId: "a", index: 0 });
  await store.move("a", { parentId: "d", index: 0 });
  expect(moveNode).not.toHaveBeenCalled();
  expect(children(queryClient, "a")).toEqual(["d"]);

  await expect(store.move("c", { parentId: null, index: 0 })).rejects.toThrow();
  expect(moveNode).not.toHaveBeenCalled();
});
