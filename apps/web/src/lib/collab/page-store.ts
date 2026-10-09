import { useMemo } from "react";
import { msg } from "@lingui/core/macro";
import type { PageMeta, PageStore } from "@slash-editor/core";
import {
  focusManager,
  useQueryClient,
  type Query,
  type QueryCacheNotifyEvent,
  type QueryClient,
} from "@tanstack/react-query";
import type * as Y from "yjs";
import {
  ApiError,
  attachmentUrl,
  createDocument,
  deleteNode,
  isMarkdownFile,
  moveNode,
  renameNode,
  restoreNode,
  searchNodes,
  shareAttachmentUrl,
  type PageSummary,
} from "@/lib/api";
import { i18n } from "@/lib/i18n";
import { keys } from "@/lib/queries/keys";
import {
  pageBacklinksOptions,
  pageChildrenOptions,
  pageLineageOptions,
  pageScope,
  type PageSource,
  type PpPageMeta,
} from "@/lib/queries/pages";
import { MIN_QUERY_LENGTH } from "@/lib/queries/search";
import { nodeOptions } from "@/lib/queries/nodes";
import { fetchSpaces } from "@/lib/queries/spaces";
import { ATTACHMENT_SCHEME } from "@/lib/collab/attachments";

export type { PageSource, PpPageMeta } from "@/lib/queries/pages";

/** The room's `Y.Map` holding the open page's `icon` and `cover` (M43). */
// eslint-disable-next-line lingui/no-unlocalized-strings -- Y.Doc map name, shared with the server
export const ROOM_META = "meta";

/** The name a page gets when nobody gave it a title. */
const UNTITLED = msg`Untitled`;
const READ_ONLY = msg`Pages cannot be changed through a share link.`;
const NOT_OPEN = msg`Only the open page’s icon and cover can be changed.`;
const NO_TOP_LEVEL = msg`A page can only be moved under another page.`;

/* eslint-disable lingui/no-unlocalized-strings -- file extension, Y.Map keys, search filter */
const MD_EXT = ".md";
const ICON = "icon";
const COVER = "cover";
const FILE_KIND = "file";
const MARKDOWN = "markdown";
/* eslint-enable lingui/no-unlocalized-strings */

/** How many pages the `@` menu lists. */
const SEARCH_LIMIT = 6;

/** How many parent links `isWithin` follows: the server's chain stops at the same depth. */
const MAX_PAGE_DEPTH = 100;

/**
 * slash-editor's `PageStore` over the TanStack Query cache, plus what pp-hub
 * does to pages outside the editor's contract.
 */
export interface PpPageStore extends PageStore {
  readonly source: PageSource;
  /** `false` for a share link: `create` and `update` reject. */
  readonly canWrite: boolean;
  /**
   * The open page's live icon and cover: the room's `meta` map. While bound,
   * `peek(pageId)` reads them from the map, and `update(pageId, {icon, cover})`
   * writes them there — the room's write-back puts them in the frontmatter.
   * Returns the unbind.
   */
  bindMeta(pageId: string, meta: Y.Map<unknown>): () => void;
  /** Moves a page to the trash (a sub-page block was deleted). */
  trash(pageId: string): Promise<void>;
  /** Takes a page back out of the trash if it is there (undo, paste). */
  restore(pageId: string): Promise<void>;
  /** `PageStore.setTrashed`, always present: trashes or restores each page. */
  setTrashed(ids: readonly string[], trashed: boolean): Promise<void>;
  /** `PageStore.move`, always present: re-parents and/or reorders a page. */
  move(pageId: string, target: { parentId: string | null; index: number }): Promise<void>;
  /** Where the page's cover image is served, if it has one. */
  coverUrl(page: PageMeta): string | undefined;
  /**
   * Warms what opening the page reads — its node and its lineage — so a hover
   * or focus on a link to it saves the round trips of the click. A share link
   * has no editor to open the page in: nothing to warm.
   */
  prefetch(pageId: string): void;
}

/** A node's name as a page title: without `.md`, whatever its case. */
export function titleFromName(name: string): string {
  return name.toLowerCase().endsWith(MD_EXT) ? name.slice(0, -MD_EXT.length) : name;
}

/**
 * The `PageStore` slash-editor's page nodes, the page header, tree,
 * breadcrumb and backlinks read, for `source`.
 *
 * `peek(id)` is `keys.page(scope, id)` straight from the cache. Every page
 * answer writes into those entries, and structural sharing keeps an entry's
 * object as it was when an answer changes nothing, so `peek` returns the same
 * object until that page actually changes. `subscribe` listens to the query
 * cache and fires — once per microtask, however many entries moved — only
 * when the data under this scope changed, or the bound `meta` map did.
 *
 * Titles and the tree are not realtime: a listened-to store refetches its
 * stale answers when the window regains focus and whenever anything
 * invalidates `keys.pages` (a rename in the drive, say).
 */
export function createPageStore(queryClient: QueryClient, source: PageSource): PpPageStore {
  const scope = pageScope(source);
  const root = keys.pageScope(scope);
  const canWrite = source.kind === "node";
  // Nothing observes a `page` entry — `peek` reads it — so the default
  // five-minute collection would drop it under a mounted block, which would
  // then flash "loading" and fetch the page again. Pages are small.
  queryClient.setQueryDefaults(keys.pages, { gcTime: Infinity });
  const listeners = new Set<() => void>();
  /** Last data seen per query hash: a cache event that changed nothing notifies no one. */
  const seen = new Map<string, unknown>();
  /** Per page, the mutation in flight: trash → restore must reach the server in order. */
  const chains = new Map<string, Promise<unknown>>();
  /** Seeding requests in flight, which `load` waits for before fetching a page alone. */
  const seeding = new Set<Promise<unknown>>();
  /** Per page, the name the latest rename asks for. */
  const renaming = new Map<string, string>();
  let bound: { pageId: string; meta: Y.Map<unknown> } | null = null;
  let overlay: { base: PageMeta; icon: string; cover: string; page: PageMeta } | null = null;
  let detach: (() => void) | null = null;
  let notifyQueued = false;
  let refreshQueued = false;

  const notify = () => {
    if (notifyQueued) return;
    notifyQueued = true;
    queueMicrotask(() => {
      notifyQueued = false;
      listeners.forEach((listener) => listener());
    });
  };

  const inScope = (query: Query) => root.every((part, index) => query.queryKey[index] === part);

  /**
   * Refetches every stale answer in scope that anyone has asked for; the
   * `page` entries are seeded, never fetched. In-flight requests are left to
   * finish.
   */
  const refresh = () => {
    if (refreshQueued) return;
    refreshQueued = true;
    queueMicrotask(() => {
      refreshQueued = false;
      void queryClient.refetchQueries(
        {
          queryKey: root,
          type: "all",
          stale: true,
          predicate: (query) => query.queryKey[2] !== "page",
        },
        { cancelRefetch: false },
      );
    });
  };

  const onCacheEvent = (event: QueryCacheNotifyEvent) => {
    if (!inScope(event.query)) return;
    const hash = event.query.queryHash;
    if (event.type === "removed") {
      seen.delete(hash);
      notify();
      return;
    }
    if (event.type !== "updated") return;
    if (event.action.type === "invalidate") refresh();
    const data = event.query.state.data;
    if (seen.get(hash) === data) return;
    seen.set(hash, data);
    notify();
  };

  const attach = () => {
    const unsubscribeCache = queryClient.getQueryCache().subscribe(onCacheEvent);
    const unsubscribeFocus = focusManager.subscribe((focused) => {
      if (focused) refresh();
    });
    return () => {
      unsubscribeCache();
      unsubscribeFocus();
    };
  };

  const cached = (pageId: string) => queryClient.getQueryData<PpPageMeta>(keys.page(scope, pageId));

  /** The bound page with the room's icon and cover over the server's; one object per change. */
  const withMeta = (page: PageMeta): PageMeta => {
    if (!bound || page.id !== bound.pageId) return page;
    const icon = stringAt(bound.meta, ICON);
    const cover = stringAt(bound.meta, COVER);
    if (overlay?.base === page && overlay.icon === icon && overlay.cover === cover) {
      return overlay.page;
    }
    const { icon: _icon, cover: _cover, ...rest } = page;
    const next: PageMeta = { ...rest, ...(icon && { icon }), ...(cover && { cover }) };
    overlay = { base: page, icon, cover, page: next };
    return next;
  };

  const peek = (pageId: string) => {
    const page = cached(pageId);
    return page && withMeta(page);
  };

  const patch = (pageId: string, change: (page: PpPageMeta) => PpPageMeta) => {
    queryClient.setQueryData<PpPageMeta>(keys.page(scope, pageId), (page) =>
      page ? change(page) : page,
    );
  };

  const setTitle = (pageId: string, title: string) => patch(pageId, (page) => ({ ...page, title }));

  /** Out of the trash: the key is dropped, as `toPageMeta` would leave it out. */
  const setTrashed = (pageId: string, trashed: boolean) =>
    patch(pageId, ({ trashed: _trashed, ...page }) => (trashed ? { ...page, trashed } : page));

  /** `pageId` itself or a page under it, by the cached parent links. */
  const isWithin = (pageId: string | null | undefined, ancestorId: string) => {
    for (let id = pageId, hops = 0; id && hops < MAX_PAGE_DEPTH; hops++) {
      if (id === ancestorId) return true;
      id = cached(id)?.parentId;
    }
    return false;
  };

  const childrenKey = (pageId: string) => keys.pageChildren(scope, pageId);

  /** Edits a cached listing of sub-pages; an unloaded one has nothing to edit. */
  const setChildren = (pageId: string, change: (pages: PageSummary[]) => PageSummary[]) =>
    queryClient.setQueryData<PageSummary[]>(childrenKey(pageId), (pages) => pages && change(pages));

  /** Runs `step` after whatever is in flight for `pageId`, whether that settled or failed. */
  const enqueue = <T>(pageId: string, step: () => Promise<T>): Promise<T> => {
    const previous = chains.get(pageId) ?? Promise.resolve();
    const next = previous.catch(() => undefined).then(step);
    chains.set(pageId, next);
    void next
      .catch(() => undefined)
      .then(() => {
        if (chains.get(pageId) === next) chains.delete(pageId);
      });
    return next;
  };

  /** A rename, create or trash moved titles or the tree: refetch everything in scope. */
  const invalidate = (spaceId?: string) => {
    void queryClient.invalidateQueries({ queryKey: keys.pages });
    // The drive shows a sub-page count on documents.
    if (spaceId) void queryClient.invalidateQueries({ queryKey: ["children", spaceId] });
  };

  const track = <T>(request: Promise<T>): Promise<T> => {
    seeding.add(request);
    void request.finally(() => seeding.delete(request)).catch(() => undefined);
    return request;
  };

  const readOnly = () => Promise.reject(new Error(i18n._(READ_ONLY)));

  const store: PpPageStore = {
    source,
    canWrite,

    async create({ parentId, title }) {
      if (source.kind !== "node" || !source.spaceId) return readOnly();
      const spaceId = source.spaceId;
      const name = `${title?.trim() || i18n._(UNTITLED)}${MD_EXT}`;
      const node = await createDocument(spaceId, MARKDOWN, name, parentId ?? undefined);
      const page: PpPageMeta = { id: node.id, parentId, title: titleFromName(node.name) };
      queryClient.setQueryData(keys.page(scope, node.id), page);
      if (parentId) patch(parentId, (parent) => ({ ...parent, hasChildren: true }));
      invalidate(spaceId);
      return page;
    },

    async update(pageId, change) {
      if (!canWrite) return readOnly();
      if (change.icon !== undefined || change.cover !== undefined) {
        if (!bound || bound.pageId !== pageId) throw new Error(i18n._(NOT_OPEN));
        const { meta } = bound;
        const write = () => {
          if (change.icon !== undefined) setOrDelete(meta, ICON, change.icon);
          if (change.cover !== undefined) setOrDelete(meta, COVER, change.cover);
        };
        if (meta.doc) meta.doc.transact(write);
        else write();
      }
      const title = change.title;
      if (title === undefined) return;
      const name = `${title.trim() || i18n._(UNTITLED)}${MD_EXT}`;
      // Every chip, block and tree row shows the new title at once; the
      // rename catches up behind it, one at a time per page.
      const before = cached(pageId)?.title;
      renaming.set(pageId, name);
      setTitle(pageId, titleFromName(name));
      await enqueue(pageId, async () => {
        // Only the last title typed is restored on failure; an earlier
        // rename failing must not flash its title back over a newer one.
        const latest = () => renaming.get(pageId) === name;
        try {
          await renameNode(pageId, name);
        } catch (error) {
          if (latest()) {
            renaming.delete(pageId);
            if (before !== undefined) setTitle(pageId, before);
          }
          throw error;
        }
        if (latest()) renaming.delete(pageId);
        void queryClient.invalidateQueries({ queryKey: keys.node(pageId) });
        invalidate(source.kind === "node" ? source.spaceId : undefined);
      });
    },

    peek,

    async load(pageId) {
      // A listing that is already on its way names this page too.
      if (seeding.size) await Promise.allSettled(seeding);
      if (cached(pageId)) return peek(pageId);
      try {
        await queryClient.fetchQuery(pageLineageOptions(source, pageId));
      } catch (error) {
        if (error instanceof ApiError && (error.status === 404 || error.status === 403)) {
          return undefined;
        }
        throw error;
      }
      return peek(pageId);
    },

    async listChildren(parentId) {
      if (!parentId) return [];
      const pages = await track(queryClient.fetchQuery(pageChildrenOptions(source, parentId)));
      return pages.map((summary) => peek(summary.id)).filter((page) => page !== undefined);
    },

    async search(query, { signal }) {
      const needle = query.trim().toLocaleLowerCase();
      if (source.kind !== "node" || !source.spaceId || needle.length < MIN_QUERY_LENGTH) return [];
      const spaceId = source.spaceId;
      // Search is member-scoped: a grantee from outside the space gets no pages.
      const spaces = await fetchSpaces(queryClient);
      if (signal.aborted || !spaces.some((space) => space.id === spaceId)) return [];
      const hits = await searchNodes(needle, { spaceId, kind: FILE_KIND, limit: 25 });
      // Search also answers content matches; a link names a page, so only a
      // document whose name matches is offered.
      return hits
        .filter(
          (node) =>
            isMarkdownFile(node) && titleFromName(node.name).toLocaleLowerCase().includes(needle),
        )
        .slice(0, SEARCH_LIMIT)
        .map(
          (node) =>
            cached(node.id) ?? { id: node.id, parentId: null, title: titleFromName(node.name) },
        );
    },

    ...(source.kind === "node" && {
      async backlinks(pageId: string) {
        const pages = await track(queryClient.fetchQuery(pageBacklinksOptions(pageId)));
        return pages.map((page) => page.id);
      },
    }),

    subscribe(listener) {
      listeners.add(listener);
      detach ??= attach();
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          detach?.();
          detach = null;
          seen.clear();
        }
      };
    },

    bindMeta(pageId, meta) {
      const entry = { pageId, meta };
      bound = entry;
      overlay = null;
      meta.observe(notify);
      notify();
      return () => {
        meta.unobserve(notify);
        if (bound === entry) {
          bound = null;
          overlay = null;
          notify();
        }
      };
    },

    trash(pageId) {
      if (!canWrite) return readOnly();
      return enqueue(pageId, async () => {
        setTrashed(pageId, true);
        try {
          await deleteNode(pageId);
        } catch (error) {
          setTrashed(pageId, false);
          throw error;
        }
        invalidate(source.kind === "node" ? source.spaceId : undefined);
      });
    },

    restore(pageId) {
      if (!canWrite) return readOnly();
      return enqueue(pageId, async () => {
        // Only a page this store saw go to the trash: a block arriving by
        // paste or a fresh `/page` names a live page.
        if (!cached(pageId)?.trashed) return;
        await restoreNode(pageId);
        setTrashed(pageId, false);
        invalidate(source.kind === "node" ? source.spaceId : undefined);
      });
    },

    setTrashed(ids, trashed) {
      return Promise.all(ids.map((id) => (trashed ? store.trash(id) : store.restore(id)))).then(
        () => undefined,
      );
    },

    async move(pageId, { parentId, index }) {
      if (!canWrite) return readOnly();
      if (!parentId) throw new Error(i18n._(NO_TOP_LEVEL));
      // Into itself or a descendant: nothing to do, as the contract says.
      if (isWithin(parentId, pageId)) return;
      await enqueue(pageId, async () => {
        const page = cached(pageId);
        const fromId = page?.parentId ?? null;
        const from = fromId
          ? queryClient.getQueryData<PageSummary[]>(childrenKey(fromId))
          : undefined;
        const to = queryClient.getQueryData<PageSummary[]>(childrenKey(parentId));
        const summary = from?.find((candidate) => candidate.id === pageId);
        // The tree shows the page in its new place at once; the move catches
        // up behind it and the tree is put back if it is refused.
        if (fromId) setChildren(fromId, (pages) => pages.filter((p) => p.id !== pageId));
        if (summary) {
          setChildren(parentId, (pages) => {
            const rest = pages.filter((p) => p.id !== pageId);
            rest.splice(Math.max(index, 0), 0, { ...summary, parent_id: parentId });
            return rest;
          });
        }
        patch(pageId, (current) => ({ ...current, parentId }));
        patch(parentId, (parent) => ({ ...parent, hasChildren: true }));
        try {
          await moveNode(pageId, parentId, index);
        } catch (error) {
          if (fromId && from) queryClient.setQueryData(childrenKey(fromId), from);
          if (to) queryClient.setQueryData(childrenKey(parentId), to);
          if (page) queryClient.setQueryData(keys.page(scope, pageId), page);
          throw error;
        }
        invalidate(source.kind === "node" ? source.spaceId : undefined);
      });
    },

    prefetch(pageId) {
      if (source.kind !== "node") return;
      // `ensure`, not `prefetch`: a cached node needs no refetch on every hover.
      queryClient.ensureQueryData(nodeOptions(pageId)).catch(() => undefined);
      void queryClient.prefetchQuery(pageLineageOptions(source, pageId));
    },

    coverUrl(page) {
      // Only the page's own attachment: pp-hub does not hotlink another
      // site's image, whatever a hand-edited frontmatter says.
      if (!page.cover?.startsWith(ATTACHMENT_SCHEME)) return undefined;
      const hash = page.cover.slice(ATTACHMENT_SCHEME.length);
      return source.kind === "node"
        ? attachmentUrl(page.id, hash)
        : shareAttachmentUrl(source.token, page.id, hash);
    },
  };
  return store;
}

/** Warms what opening `pageId` reads, when `store` is pp-hub's (see `PpPageStore.prefetch`). */
export function prefetchPage(store: PageStore, pageId: string): void {
  if ("prefetch" in store) (store as PpPageStore).prefetch(pageId);
}

/** One store per source for the life of the component — the editor latches it once. */
export function usePageStore(source: PageSource): PpPageStore {
  const queryClient = useQueryClient();
  const token = source.kind === "share" ? source.token : undefined;
  const spaceId = source.kind === "node" ? source.spaceId : undefined;
  return useMemo(
    () =>
      createPageStore(
        queryClient,
        // eslint-disable-next-line lingui/no-unlocalized-strings -- source discriminants
        token !== undefined ? { kind: "share", token } : { kind: "node", spaceId },
      ),
    [queryClient, token, spaceId],
  );
}

function stringAt(meta: Y.Map<unknown>, key: string): string {
  const value = meta.get(key);
  return typeof value === "string" ? value : "";
}

function setOrDelete(meta: Y.Map<unknown>, key: string, value: string | undefined): void {
  if (value) meta.set(key, value);
  else meta.delete(key);
}
