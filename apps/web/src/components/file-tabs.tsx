import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { ReactNode } from "react";
import {
  Link,
  useNavigate,
  useParams,
  useRouter,
  useRouterState,
  useSearch,
} from "@tanstack/react-router";
import { useQueries } from "@tanstack/react-query";
import { useLingui } from "@lingui/react/macro";
import { X } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { ApiError, isOfficeFile, type Node } from "@/lib/api";
import { nodeOptions } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { closeTab, liveTabs, showNode, type FileTab } from "@/lib/file-tabs";
import { takeSameTab, type FileSearch } from "@/lib/open-file";
import { DocActionsSlot, OnScreen } from "@/components/header-slot";
import { NodeIcon } from "@/components/node-icon";
import { FileView } from "@/components/file-view";

/** The tab scope of files shared from outside any of the caller's spaces. */
// eslint-disable-next-line lingui/no-unlocalized-strings -- scope id, not UI text
const SHARED = "shared";
const NO_TABS: FileTab[] = [];
const NO_SEARCH: FileSearch = {};
/** How often the shell looks for office files left out of sight too long. */
const IDLE_CHECK_MS = 60_000;

/**
 * Open files as tabs inside the drive shell. The URL names the file on
 * screen (`/drive/$spaceId/file/$nodeId`); the tab list is this person's,
 * per space, and survives reloads (`ui-store.fileTabs`). The file routes
 * render nothing themselves: `FileTabsHost` keeps the recently shown files
 * mounted out of sight, so going back to one is instant — the document's
 * room, the sheet's OnlyOffice frame, the scroll position are all still
 * there — and `FileTabBar` lists them in the document bar under the header.
 *
 * Which tab a navigation lands in: a file already open is focused; a page
 * link followed inside a document (`openFile(…, { sameTab })`) and the
 * browser's back/forward retarget the tab on screen; anything else opens a
 * tab next to it.
 */
interface FileTabsValue {
  /** A space id, `shared`, or null outside both (no tabs). */
  scope: string | null;
  tabs: FileTab[];
  /** The tab on screen; undefined on a drive surface. */
  activeKey: string | undefined;
  /** Whether the URL names a file, before its tab is known. */
  onFile: boolean;
  live: Set<string>;
  nodes: Record<string, Node | undefined>;
  searchOf: (key: string) => FileSearch;
  close: (key: string) => void;
}

const FileTabsContext = createContext<FileTabsValue | null>(null);

function useFileTabs(): FileTabsValue {
  const value = useContext(FileTabsContext);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- developer-facing invariant
  if (!value) throw new Error("useFileTabs outside FileTabsProvider");
  return value;
}

function tabLocation(scope: string, nodeId: string) {
  return scope === SHARED
    ? ({ to: "/drive/shared/file/$nodeId", params: { nodeId } } as const)
    : ({ to: "/drive/$spaceId/file/$nodeId", params: { spaceId: scope, nodeId } } as const);
}

export function FileTabsProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const navigate = useNavigate();
  // Params and the location from the same, resolved, navigation: mid-load the
  // pending location is already the next page while the params are still
  // this one's, and pairing them would reopen a tab just closed.
  const { spaceId, nodeId } = useParams({ strict: false });
  const resolved = useRouterState({ select: (s) => s.resolvedLocation });
  const pathname = resolved?.pathname ?? "";
  const href = resolved?.href ?? "";
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
  const scope = spaceId ?? (pathname.startsWith("/drive/shared") ? SHARED : null);
  const onFile = scope !== null && nodeId !== undefined;
  const { thread, subject } = useSearch({ strict: false });

  const tabs = useUiStore((s) => (scope ? s.fileTabs[scope] : undefined)) ?? NO_TABS;
  const setFileTabs = useUiStore((s) => s.setFileTabs);
  const activeKey = onFile ? tabs.find((tab) => tab.nodeId === nodeId)?.key : undefined;

  // Runtime bookkeeping, none of it worth a render of its own.
  const recent = useRef<string[]>([]);
  const hiddenSince = useRef(new Map<string, number>());
  const searches = useRef(new Map<string, FileSearch>());
  const lastShown = useRef(new Map<string, string>());
  const driveHrefs = useRef(new Map<string, string>());
  const onScreenKey = useRef<string | undefined>(undefined);
  const popped = useRef(false);

  useEffect(
    () =>
      router.history.subscribe(({ action }) => {
        popped.current =
          action.type === "BACK" || action.type === "FORWARD" || action.type === "GO";
      }),
    [router],
  );

  // Before paint, so a file never flashes an empty column while its tab is made.
  useLayoutEffect(() => {
    const was = onScreenKey.current;
    const same = popped.current;
    popped.current = false;
    if (!scope) return;
    if (!nodeId) {
      driveHrefs.current.set(scope, href);
      lastShown.current.delete(scope);
      if (was) hiddenSince.current.set(was, Date.now());
      onScreenKey.current = undefined;
      return;
    }
    const current = useUiStore.getState().fileTabs[scope] ?? NO_TABS;
    const next = showNode(current, nodeId, {
      fromKey: lastShown.current.get(scope),
      same: takeSameTab(nodeId) || same,
      newKey: crypto.randomUUID(),
    });
    if (next.tabs !== current) setFileTabs(scope, next.tabs);
    lastShown.current.set(scope, next.key);
    searches.current.set(next.key, { thread, subject });
    recent.current = [next.key, ...recent.current.filter((key) => key !== next.key)];
    if (was && was !== next.key) hiddenSince.current.set(was, Date.now());
    hiddenSince.current.delete(next.key);
    onScreenKey.current = next.key;
  }, [scope, nodeId, thread, subject, href, setFileTabs]);

  const queries = useQueries({ queries: tabs.map((tab) => nodeOptions(tab.nodeId)) });
  const nodes: Record<string, Node | undefined> = {};
  const gone: string[] = [];
  tabs.forEach((tab, i) => {
    const query = queries[i];
    nodes[tab.nodeId] = query?.data;
    const error = query?.error;
    if (error instanceof ApiError && (error.status === 403 || error.status === 404)) {
      gone.push(tab.key);
    }
  });

  // A restored tab whose file was deleted or unshared since goes quietly; the
  // one on screen stays to say why it cannot open.
  const goneKey = gone.filter((key) => key !== activeKey).join();
  useEffect(() => {
    if (!scope || !goneKey) return;
    const drop = new Set(goneKey.split(","));
    setFileTabs(
      scope,
      (useUiStore.getState().fileTabs[scope] ?? NO_TABS).filter((tab) => !drop.has(tab.key)),
    );
  }, [scope, goneKey, setFileTabs]);

  // Office files out of sight are let go after a while; look once a minute.
  const [now, setNow] = useState(() => Date.now());
  const anyTabs = tabs.length > 0;
  useEffect(() => {
    if (!anyTabs) return;
    const timer = setInterval(() => setNow(Date.now()), IDLE_CHECK_MS);
    return () => clearInterval(timer);
  }, [anyTabs]);

  const live = liveTabs({
    tabs,
    recent: recent.current,
    activeKey,
    isOffice: (tab) => {
      const node = nodes[tab.nodeId];
      return node?.kind === "file" && isOfficeFile(node.name);
    },
    hiddenSince: (key) => hiddenSince.current.get(key),
    now,
  });

  // Closing the last tab on screen goes back to the drive surface last shown.
  const driveHref =
    (scope && driveHrefs.current.get(scope)) ||
    // eslint-disable-next-line lingui/no-unlocalized-strings -- URL paths
    (scope === SHARED ? "/drive/shared" : `/drive/${scope ?? ""}`);

  const close = useCallback(
    (key: string) => {
      if (!scope) return;
      const current = useUiStore.getState().fileTabs[scope] ?? NO_TABS;
      const { tabs: rest, neighbour } = closeTab(current, key);
      setFileTabs(scope, rest);
      recent.current = recent.current.filter((k) => k !== key);
      hiddenSince.current.delete(key);
      searches.current.delete(key);
      if (key !== activeKey) return;
      if (neighbour) {
        void navigate({
          ...tabLocation(scope, neighbour.nodeId),
          search: searches.current.get(neighbour.key) ?? NO_SEARCH,
          replace: true,
        });
      } else {
        void navigate({ href: driveHref, replace: true });
      }
    },
    [scope, activeKey, driveHref, navigate, setFileTabs],
  );

  const value: FileTabsValue = {
    scope,
    tabs,
    activeKey,
    onFile,
    live,
    nodes,
    searchOf: (key) => searches.current.get(key) ?? NO_SEARCH,
    close,
  };

  return <FileTabsContext.Provider value={value}>{children}</FileTabsContext.Provider>;
}

/**
 * The open files, mounted. Only the tab on screen is visible, and only it
 * reaches the header (`OnScreen`). Slots render in tab order, never in
 * recency order: moving an iframe in the DOM reloads it, and React moves a
 * keyed child whose position among its siblings changes.
 */
export function FileTabsHost() {
  const { tabs, activeKey, live, searchOf } = useFileTabs();
  return (
    <div className={cn("flex min-h-0 flex-1 flex-col", !activeKey && "hidden")}>
      {tabs
        .filter((tab) => live.has(tab.key))
        .map((tab) => {
          const { thread, subject } = searchOf(tab.key);
          return (
            <TabSlot
              key={tab.key}
              nodeId={tab.nodeId}
              onScreen={tab.key === activeKey}
              thread={thread}
              subject={subject}
            />
          );
        })}
    </div>
  );
}

const TabSlot = memo(function TabSlot({
  nodeId,
  onScreen,
  thread,
  subject,
}: { nodeId: string; onScreen: boolean } & FileSearch) {
  return (
    <OnScreen value={onScreen}>
      <div className={cn("flex min-h-0 flex-1 flex-col", !onScreen && "hidden")}>
        <FileView nodeId={nodeId} thread={thread} subject={subject} />
      </div>
    </OnScreen>
  );
});

/* eslint-disable lingui/no-unlocalized-strings -- Tailwind classes */
// A tab: an underline marks the one on screen, as in a document's own tab row.
const TAB =
  "group -mb-px flex max-w-60 shrink-0 items-center gap-1 border-b-2 pl-2 text-xs transition-colors";
const TAB_ON_SCREEN = "border-foreground text-foreground";
const TAB_OFF_SCREEN = "text-muted-foreground hover:text-foreground border-transparent";
/* eslint-enable lingui/no-unlocalized-strings */

/**
 * The document bar, under the navigation header and shown once a file is
 * open: the open-file tabs on the left, the file on screen's own controls
 * (`DocActions` — version line, presence, outline, comments, Share,
 * Versions, Download) on the right. The header above stays navigation only.
 * A tab is a real link, so ⌘/Ctrl-click still opens the file in a browser
 * tab of its own; a middle click closes it, as it does a browser tab. The
 * drive has no tab: the rail and the breadcrumb are the way back to it.
 */
export function FileTabBar() {
  const { t } = useLingui();
  const { scope, tabs, activeKey, nodes, searchOf, close } = useFileTabs();
  const strip = useRef<HTMLElement>(null);

  // Scroll the strip alone. `scrollIntoView` would also scroll every
  // `overflow: hidden` ancestor — the shell pins the viewport with those —
  // and in headless Chromium it stalls rendering outright.
  useEffect(() => {
    const nav = strip.current;
    const el = activeKey && nav?.querySelector<HTMLElement>(`[data-tab="${activeKey}"]`);
    if (!nav || !el) return;
    const right = el.offsetLeft + el.offsetWidth;
    if (el.offsetLeft < nav.scrollLeft) nav.scrollLeft = el.offsetLeft;
    else if (right > nav.scrollLeft + nav.clientWidth) nav.scrollLeft = right - nav.clientWidth;
  }, [activeKey]);

  if (!scope || tabs.length === 0) return null;

  return (
    <div className="flex h-10 shrink-0 items-stretch gap-3 border-b px-4 lg:px-6">
      <nav
        ref={strip}
        aria-label={t`Open files`}
        className="relative -ml-2 flex min-w-0 flex-1 items-stretch gap-1 overflow-x-auto [scrollbar-width:none]"
      >
        {tabs.map((tab) => {
          const node = nodes[tab.nodeId];
          const active = tab.key === activeKey;
          const name = node?.name ?? "";
          return (
            <div
              key={tab.key}
              data-tab={tab.key}
              className={cn(TAB, active ? TAB_ON_SCREEN : TAB_OFF_SCREEN)}
            >
              <Link
                {...tabLocation(scope, tab.nodeId)}
                search={searchOf(tab.key)}
                aria-current={active ? "page" : undefined}
                title={name}
                className="flex min-w-0 items-center gap-1.5 py-1"
                onAuxClick={(event) => {
                  if (event.button !== 1) return;
                  event.preventDefault();
                  close(tab.key);
                }}
              >
                {node ? (
                  <NodeIcon node={node} className="size-3.5 shrink-0" />
                ) : (
                  <Skeleton className="size-3.5 shrink-0" />
                )}
                {node ? (
                  <span className={cn("truncate", active && "font-medium")}>{name}</span>
                ) : (
                  <Skeleton className="h-3 w-20" />
                )}
              </Link>
              <button
                type="button"
                aria-label={t`Close ${name}`}
                className={cn(
                  "hover:bg-muted text-muted-foreground hover:text-foreground rounded-sm p-0.5 focus-visible:opacity-100",
                  active ? "opacity-100" : "opacity-0 group-hover:opacity-100",
                )}
                onClick={() => close(tab.key)}
              >
                <X className="size-3" />
              </button>
            </div>
          );
        })}
      </nav>
      <DocActionsSlot />
    </div>
  );
}
