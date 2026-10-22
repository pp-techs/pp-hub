import { pageTitle, type PageMeta } from "@slash-editor/core";
import { useBreadcrumb } from "@slash-editor/react";
import { useLingui } from "@lingui/react/macro";
import { Breadcrumb, BreadcrumbList } from "@workspace/ui/components/breadcrumb";
import type { PpPageStore } from "@/lib/collab/page-store";
import { PageIcon, UNTITLED_PAGE } from "@/components/nodes/page-node-views";
import { CrumbItems, type Crumb } from "@/components/site-header";

/**
 * The documents above a page, root first, each opening its page, as crumbs for
 * the same trail the folders use (`CrumbItems` folds a long one into a `…`
 * menu, narrower on a phone). `self` is the page itself, which opens nothing.
 * Built from the store's cache (`parentId` walked upwards), so a rename shows
 * at once. `ancestors` is empty for a top-level page — or one whose parent this
 * reader cannot view, since the server then reports no parent.
 */
export function usePageCrumbs(
  store: PpPageStore,
  pageId: string,
  onNavigate: (pageId: string) => void,
): { ancestors: Crumb[]; self?: Crumb } {
  const { i18n } = useLingui();
  const trail = useBreadcrumb(store, pageId);
  const last = trail.at(-1);
  const hasSelf = last?.id === pageId;
  const toCrumb = (page: PageMeta, open: boolean): Crumb => ({
    key: page.id,
    label: pageTitle(page, i18n._(UNTITLED_PAGE)),
    icon: <PageIcon page={page} className="size-3.5" />,
    onSelect: open ? () => onNavigate(page.id) : undefined,
  });
  return {
    ancestors: (hasSelf ? trail.slice(0, -1) : trail).map((page) => toCrumb(page, true)),
    self: hasSelf && last ? toCrumb(last, false) : undefined,
  };
}

/** A page's trail on its own line, ending in the page: the share page's. */
export function PageTrail({
  store,
  pageId,
  onNavigate,
}: {
  store: PpPageStore;
  pageId: string;
  onNavigate: (pageId: string) => void;
}) {
  const { ancestors, self } = usePageCrumbs(store, pageId, onNavigate);
  if (ancestors.length === 0) return null;
  return (
    <Breadcrumb className="min-w-0">
      <BreadcrumbList className="flex-nowrap">
        <CrumbItems crumbs={self ? [...ancestors, self] : ancestors} />
      </BreadcrumbList>
    </Breadcrumb>
  );
}
