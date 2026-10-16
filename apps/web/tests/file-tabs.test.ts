import { expect, test } from "vite-plus/test";
import {
  LIVE_OFFICE_TABS,
  LIVE_TABS,
  OFFICE_IDLE_MS,
  closeTab,
  liveTabs,
  showNode,
  type FileTab,
} from "../src/lib/file-tabs.ts";

const tab = (key: string, nodeId = `n-${key}`): FileTab => ({ key, nodeId });

test("showing a node that is already open focuses its tab instead of opening a second", () => {
  const tabs = [tab("a"), tab("b")];
  const next = showNode(tabs, "n-b", { fromKey: "a", same: false, newKey: "c" });
  expect(next.key).toBe("b");
  expect(next.tabs).toBe(tabs);
});

test("a new file opens in a tab right after the one it was opened from", () => {
  const next = showNode([tab("a"), tab("b")], "n-x", { fromKey: "a", same: false, newKey: "c" });
  expect(next.key).toBe("c");
  expect(next.tabs.map((t) => t.key)).toEqual(["a", "c", "b"]);
});

test("opened from the drive, a new file goes to the end", () => {
  const next = showNode([tab("a")], "n-x", { same: false, newKey: "c" });
  expect(next.tabs.map((t) => t.key)).toEqual(["a", "c"]);
});

test("a page link retargets its own tab and keeps the tab's key", () => {
  const next = showNode([tab("a"), tab("b")], "n-sub", { fromKey: "b", same: true, newKey: "c" });
  expect(next.key).toBe("b");
  expect(next.tabs).toEqual([tab("a"), tab("b", "n-sub")]);
});

test("a page link with no tab on screen opens a tab rather than retargeting one", () => {
  const next = showNode([tab("a")], "n-sub", { same: true, newKey: "c" });
  expect(next.tabs.map((t) => t.key)).toEqual(["a", "c"]);
});

test("closing a tab hands the screen to its right neighbour, then its left, then the drive", () => {
  const tabs = [tab("a"), tab("b"), tab("c")];
  expect(closeTab(tabs, "b").neighbour?.key).toBe("c");
  expect(closeTab(tabs, "c").neighbour?.key).toBe("b");
  expect(closeTab([tab("a")], "a")).toEqual({ tabs: [], neighbour: undefined });
});

const live = (opts: Partial<Parameters<typeof liveTabs>[0]> & { tabs: FileTab[] }) =>
  [
    ...liveTabs({
      recent: opts.tabs.map((t) => t.key),
      activeKey: opts.tabs[0]?.key,
      isOffice: () => false,
      hiddenSince: () => 0,
      now: 0,
      ...opts,
    }),
  ].sort();

test("only the most recently shown tabs stay mounted", () => {
  const tabs = Array.from({ length: LIVE_TABS + 2 }, (_, i) => tab(`t${i}`));
  const kept = live({ tabs });
  expect(kept).toHaveLength(LIVE_TABS);
  expect(kept).not.toContain(`t${LIVE_TABS}`);
});

test("a tab never shown since the page loaded waits to be opened", () => {
  expect(live({ tabs: [tab("a"), tab("b")], recent: ["a"] })).toEqual(["a"]);
});

test("office files are capped, the one on screen counting toward the cap", () => {
  const tabs = [tab("doc1"), tab("doc2"), tab("doc3"), tab("md")];
  const kept = live({ tabs, isOffice: (t) => t.key.startsWith("doc") });
  expect(kept.filter((k) => k.startsWith("doc"))).toHaveLength(LIVE_OFFICE_TABS);
  expect(kept).toContain("doc1");
  expect(kept).toContain("md");
});

test("an office file out of sight too long is let go, unless it is on screen", () => {
  const tabs = [tab("md"), tab("doc")];
  const isOffice = (t: FileTab) => t.key === "doc";
  const hiddenSince = () => 0;
  expect(live({ tabs, isOffice, hiddenSince, now: OFFICE_IDLE_MS - 1 })).toEqual(["doc", "md"]);
  expect(live({ tabs, isOffice, hiddenSince, now: OFFICE_IDLE_MS })).toEqual(["md"]);
  expect(live({ tabs, isOffice, hiddenSince, now: OFFICE_IDLE_MS, activeKey: "doc" })).toEqual([
    "doc",
    "md",
  ]);
});
