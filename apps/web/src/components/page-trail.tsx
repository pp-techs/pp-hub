import { Fragment } from "react";
import type { PageMeta } from "@slash-editor/core";
import { useBreadcrumb } from "@slash-editor/react";
import {
  Breadcrumb,
  BreadcrumbEllipsis,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@workspace/ui/components/breadcrumb";
import { cn } from "@workspace/ui/lib/utils";
import type { PpPageStore } from "@/lib/collab/page-store";
import { PageIcon, usePageTitle } from "@/components/nodes/page-node-views";

/** Ancestors shown before the trail folds its middle into "…". */
const MAX_ANCESTORS = 3;

/**
 * The documents above a page, root first, each opening its page; with
 * `current`, the page itself ends the trail. Built from the store's cache
 * (`parentId` walked upwards), so a rename shows here at once. Renders
 * nothing for a top-level page — or one whose parent this reader cannot
 * view, since the server then reports no parent.
 */
export function PageTrail({
  store,
  pageId,
  onNavigate,
  current = false,
  className,
}: {
  store: PpPageStore;
  pageId: string;
  onNavigate: (pageId: string) => void;
  current?: boolean;
  className?: string;
}) {
  const trail = useBreadcrumb(store, pageId);
  const self = trail.at(-1)?.id === pageId ? trail.at(-1) : undefined;
  const ancestors = self ? trail.slice(0, -1) : trail;
  if (ancestors.length === 0) return null;
  const folded = ancestors.length > MAX_ANCESTORS;
  const shown = folded ? [ancestors[0]!, ...ancestors.slice(-(MAX_ANCESTORS - 1))] : ancestors;

  return (
    <Breadcrumb className={cn("min-w-0", className)}>
      <BreadcrumbList className="flex-nowrap">
        {shown.map((page, index) => (
          <Fragment key={page.id}>
            {index > 0 ? <BreadcrumbSeparator /> : null}
            {folded && index === 1 ? (
              <>
                <BreadcrumbItem>
                  <BreadcrumbEllipsis />
                </BreadcrumbItem>
                <BreadcrumbSeparator />
              </>
            ) : null}
            <BreadcrumbItem className="min-w-0">
              <BreadcrumbLink
                render={<button type="button" onClick={() => onNavigate(page.id)} />}
                className="flex min-w-0 max-w-40 items-center gap-1"
              >
                <TrailEntry page={page} />
              </BreadcrumbLink>
            </BreadcrumbItem>
          </Fragment>
        ))}
        <BreadcrumbSeparator />
        {current && self ? (
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbPage className="flex min-w-0 max-w-60 items-center gap-1">
              <TrailEntry page={self} />
            </BreadcrumbPage>
          </BreadcrumbItem>
        ) : null}
      </BreadcrumbList>
    </Breadcrumb>
  );
}

function TrailEntry({ page }: { page: PageMeta }) {
  const title = usePageTitle(page);
  return (
    <>
      <PageIcon page={page} className="size-3.5" />
      <span className="truncate">{title}</span>
    </>
  );
}
