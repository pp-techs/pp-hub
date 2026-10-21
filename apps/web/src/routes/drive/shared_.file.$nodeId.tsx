import { createFileRoute } from "@tanstack/react-router";
import { msg } from "@lingui/core/macro";
import { nodeOptions } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { validateFileSearch } from "@/lib/open-file";
import { FileView } from "@/components/file-view";

/** A file shared from a space the caller is not a member of, rendered in the drive shell. */
export const Route = createFileRoute("/drive/shared_/file/$nodeId")({
  head: () => ({ meta: [{ title: pageTitle(msg`Opening`) }] }),
  validateSearch: validateFileSearch,
  loader: ({ context, params }) => {
    void context.queryClient.ensureQueryData(nodeOptions(params.nodeId));
  },
  component: FilePage,
});

function FilePage() {
  const { nodeId } = Route.useParams();
  const { thread, subject } = Route.useSearch();
  return <FileView nodeId={nodeId} thread={thread} subject={subject} />;
}
