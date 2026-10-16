/**
 * The open-file tabs of the drive shell, as data. A tab is a slot with a
 * stable `key` showing one node; following a page link inside a document
 * retargets the slot (`nodeId` changes, `key` does not), so the screen keeps
 * its editor while the next page loads. Every function here is pure; the
 * shell (`components/file-tabs.tsx`) owns the state and the clock.
 */
export interface FileTab {
  key: string;
  nodeId: string;
}

/** How many open files stay mounted at once; the rest reload when shown. */
export const LIVE_TABS = 5;
/**
 * How many of those may be office files. An OnlyOffice frame is the heaviest
 * thing on the page, and a mounted one holds the file's WOPI lock, which
 * refuses a new version uploaded or saved over WebDAV.
 */
export const LIVE_OFFICE_TABS = 2;
/** How long an office file stays mounted out of sight before its lock is let go. */
export const OFFICE_IDLE_MS = 10 * 60 * 1000;

/**
 * The tabs after `nodeId` is shown, and the key showing it. An open tab with
 * the node wins. Otherwise `same` retargets the tab `fromKey` (a page link,
 * the back button) and anything else opens a new tab `newKey` after it.
 */
export function showNode(
  tabs: FileTab[],
  nodeId: string,
  { fromKey, same, newKey }: { fromKey?: string; same: boolean; newKey: string },
): { tabs: FileTab[]; key: string } {
  const open = tabs.find((tab) => tab.nodeId === nodeId);
  if (open) return { tabs, key: open.key };
  const from = fromKey === undefined ? -1 : tabs.findIndex((tab) => tab.key === fromKey);
  if (same && from !== -1) {
    return {
      tabs: tabs.map((tab, i) => (i === from ? { ...tab, nodeId } : tab)),
      key: tabs[from]!.key,
    };
  }
  const at = from === -1 ? tabs.length : from + 1;
  return { tabs: [...tabs.slice(0, at), { key: newKey, nodeId }, ...tabs.slice(at)], key: newKey };
}

/**
 * The tabs after `key` is closed, and the tab to show in its place when it
 * was the one on screen: its right-hand neighbour, else its left, else none
 * (the drive).
 */
export function closeTab(
  tabs: FileTab[],
  key: string,
): { tabs: FileTab[]; neighbour: FileTab | undefined } {
  const at = tabs.findIndex((tab) => tab.key === key);
  if (at === -1) return { tabs, neighbour: undefined };
  const rest = tabs.filter((tab) => tab.key !== key);
  return { tabs: rest, neighbour: rest[at] ?? rest[at - 1] };
}

/**
 * Which tabs stay mounted: the most recently shown first, up to `LIVE_TABS`,
 * of which at most `LIVE_OFFICE_TABS` office files — and no office file left
 * out of sight longer than `OFFICE_IDLE_MS`. The tab on screen always stays.
 * A tab never shown since the page loaded (restored from storage) is not in
 * `recent` and waits to be opened.
 */
export function liveTabs({
  tabs,
  recent,
  activeKey,
  isOffice,
  hiddenSince,
  now,
}: {
  tabs: FileTab[];
  /** Keys, most recently shown first. */
  recent: string[];
  activeKey: string | undefined;
  isOffice: (tab: FileTab) => boolean;
  /** When a tab last left the screen; absent while it is on screen. */
  hiddenSince: (key: string) => number | undefined;
  now: number;
}): Set<string> {
  const byKey = new Map(tabs.map((tab) => [tab.key, tab]));
  const order = activeKey ? [activeKey, ...recent.filter((key) => key !== activeKey)] : recent;
  const live = new Set<string>();
  let office = 0;
  for (const key of order) {
    if (live.size === LIVE_TABS) break;
    const tab = byKey.get(key);
    if (!tab) continue;
    const heavy = isOffice(tab);
    if (key !== activeKey && heavy) {
      const since = hiddenSince(key);
      if (office === LIVE_OFFICE_TABS) continue;
      if (since !== undefined && now - since >= OFFICE_IDLE_MS) continue;
    }
    if (heavy) office += 1;
    live.add(key);
  }
  return live;
}
