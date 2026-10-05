import type { PageMeta } from "@slash-editor/core";
import { useQuery, type QueryClient } from "@tanstack/react-query";
import {
  ApiError,
  getNodeBacklinks,
  getNodePage,
  getNodePages,
  getSharePage,
  getSharePages,
  type PageLineage,
  type PageSummary,
} from "@/lib/api";
import { keys } from "./keys";

// Pages (M43): the page tree is the node tree, read through the page routes.
// Every answer is also written page by page into `keys.page`, which is what
// the page store's synchronous `peek` reads — so a sub-page block, a link
// chip and a tree row all show the same title the moment any one of these
// queries learns it.

/**
 * Who is reading: a signed-in user (`spaceId` is the space `search` and
 * `create` work in), or a share link's anonymous visitor.
 */
export type PageSource = { kind: "node"; spaceId?: string } | { kind: "share"; token: string };

/** The cache scope of a source — see `keys.pageScope`. */
export function pageScope(source: PageSource): string {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- cache key segment
  return source.kind === "node" ? "node" : `share:${source.token}`;
}

/** slash-editor's `PageMeta`, plus whether the tree should offer to expand the row. */
export interface PpPageMeta extends PageMeta {
  hasChildren?: boolean;
}

/** How long a page answer counts as fresh: titles move by refetch, not in real time. */
const PAGE_STALE_TIME = 30_000;

/** A 4xx is the answer (not markdown, no access, gone), not a blip worth retrying. */
const retryPage = (failures: number, error: Error) =>
  failures < 2 && !(error instanceof ApiError && error.status >= 400 && error.status < 500);

/**
 * The client shape of a page. Absent values are left out rather than set to
 * `undefined`, so an unchanged answer compares deep-equal and structural
 * sharing keeps the cached object — `peek`'s identity — as it was.
 */
export function toPageMeta(summary: PageSummary): PpPageMeta {
  const page: PpPageMeta = {
    id: summary.id,
    parentId: summary.parent_id,
    title: summary.title,
  };
  if (summary.icon) page.icon = summary.icon;
  if (summary.cover) page.cover = summary.cover;
  if (summary.trashed) page.trashed = true;
  if (summary.has_children) page.hasChildren = true;
  return page;
}

/** Writes each page into its own `keys.page` entry. */
export function seedPages(client: QueryClient, scope: string, pages: PageSummary[]): void {
  for (const summary of pages)
    client.setQueryData(keys.page(scope, summary.id), toPageMeta(summary));
}

/** A page and its ancestors, root first. 404 for a node that is not a page the reader can view. */
export function pageLineageOptions(source: PageSource, pageId: string) {
  const scope = pageScope(source);
  return {
    queryKey: keys.pageLineage(scope, pageId),
    queryFn: async ({ client }: { client: QueryClient }): Promise<PageLineage> => {
      const lineage =
        source.kind === "node"
          ? await getNodePage(pageId)
          : await getSharePage(source.token, pageId);
      seedPages(client, scope, [...lineage.ancestors, lineage.page]);
      return lineage;
    },
    staleTime: PAGE_STALE_TIME,
    retry: retryPage,
  };
}

export function usePageLineage(source: PageSource, pageId: string | null) {
  return useQuery({ ...pageLineageOptions(source, pageId ?? ""), enabled: pageId !== null });
}

/** A page's direct, live sub-pages, oldest first. */
export function pageChildrenOptions(source: PageSource, pageId: string) {
  const scope = pageScope(source);
  return {
    queryKey: keys.pageChildren(scope, pageId),
    queryFn: async ({ client }: { client: QueryClient }): Promise<PageSummary[]> => {
      const pages =
        source.kind === "node"
          ? await getNodePages(pageId)
          : await getSharePages(source.token, pageId);
      seedPages(client, scope, pages);
      return pages;
    },
    staleTime: PAGE_STALE_TIME,
    retry: retryPage,
  };
}

/** Documents linking to the page. Signed-in only: a share link has no backlinks. */
export function pageBacklinksOptions(pageId: string) {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- cache scope
  const scope = "node";
  return {
    queryKey: keys.pageBacklinks(scope, pageId),
    queryFn: async ({ client }: { client: QueryClient }): Promise<PageSummary[]> => {
      const pages = await getNodeBacklinks(pageId);
      seedPages(client, scope, pages);
      return pages;
    },
    staleTime: PAGE_STALE_TIME,
    retry: retryPage,
  };
}
