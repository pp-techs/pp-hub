import { createFileRoute } from "@tanstack/react-router";
import { msg } from "@lingui/core/macro";
import { nodeOptions } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { validateFileSearch } from "@/lib/open-file";

/**
 * A file of this space, open in the shell. The route only names it: the
 * drive shell's tab host (`components/file-tabs.tsx`) draws it, and keeps it
 * mounted when another tab takes the screen.
 */
export const Route = createFileRoute("/drive/$spaceId/file/$nodeId")({
  head: () => ({ meta: [{ title: pageTitle(msg`Opening`) }] }),
  validateSearch: validateFileSearch,
  loader: ({ context, params }) => {
    void context.queryClient.ensureQueryData(nodeOptions(params.nodeId));
  },
});
