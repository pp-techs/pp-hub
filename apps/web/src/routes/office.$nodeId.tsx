import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Trans } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { nodeOptions, requireAuth } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { faviconFor } from "@/components/node-icon";
import { EditorFrame, useOfficeSession } from "@/components/office-frame";
import { useDocumentTitle, useFavicon } from "@/lib/use-page-head";

/**
 * An office file on a page of its own: OnlyOffice's editor filling the
 * window, with no rail, header or tab strip around it. It is where an office
 * file lands when the account opens them in a new browser tab
 * (`openFile`), so the editor gets the whole height of the window.
 */
export const Route = createFileRoute("/office/$nodeId")({
  head: () => ({ meta: [{ title: pageTitle(msg`Opening`) }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
  },
  component: OfficePage,
});

function OfficePage() {
  const { nodeId } = Route.useParams();
  const node = useQuery(nodeOptions(nodeId));
  const { session, error } = useOfficeSession(nodeId);

  useDocumentTitle(node.data?.name);
  useFavicon(node.data ? faviconFor(node.data.name) : undefined);

  const failure = node.error?.message ?? error;
  return (
    <main className="flex h-svh flex-col">
      {failure ? (
        <p className="text-destructive flex flex-1 items-center justify-center text-sm">
          {failure}
        </p>
      ) : !session ? (
        <p className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
          <Trans>Opening editor…</Trans>
        </p>
      ) : (
        <EditorFrame key={session.token} nodeId={nodeId} session={session} />
      )}
    </main>
  );
}
