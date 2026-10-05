import { Trans } from "@lingui/react/macro";
import type { PageMeta } from "@slash-editor/core";
import { useBacklinks } from "@slash-editor/react";
import { Button } from "@workspace/ui/components/button";
import type { PpPageStore } from "@/lib/collab/page-store";
import { PageIcon, usePageTitle } from "@/components/nodes/page-node-views";

/**
 * The documents that link to this page or hold it as a sub-page, under the
 * document — the ones this reader can view, from the server's index of
 * every page's references (refreshed when a version is written, so a link
 * made a moment ago shows once the room has saved). Nothing at all when no
 * page links here.
 */
export function PageBacklinks({
  store,
  pageId,
  onNavigate,
}: {
  store: PpPageStore;
  pageId: string;
  onNavigate: (pageId: string) => void;
}) {
  const { pages } = useBacklinks(store, pageId);
  if (pages.length === 0) return null;

  return (
    <section className="border-border mt-10 flex flex-col gap-1 border-t pt-4">
      <h2 className="text-muted-foreground px-1 text-xs font-medium">
        <Trans>Linked from</Trans>
      </h2>
      <ul className="flex flex-col">
        {pages.map((page) => (
          <li key={page.id}>
            <Backlink page={page} onNavigate={onNavigate} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function Backlink({ page, onNavigate }: { page: PageMeta; onNavigate: (pageId: string) => void }) {
  const title = usePageTitle(page);
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="w-full justify-start"
      onClick={() => onNavigate(page.id)}
    >
      <PageIcon page={page} className="size-4" />
      <span className="truncate">{title}</span>
    </Button>
  );
}
