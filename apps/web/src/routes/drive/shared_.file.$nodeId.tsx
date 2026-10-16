import { createFileRoute } from "@tanstack/react-router";
import { msg } from "@lingui/core/macro";
import { nodeOptions } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { validateFileSearch } from "@/lib/open-file";

/**
 * A file shared from a space the caller is not a member of, open in the
 * shell. Outside `/drive/$spaceId` for the reason `/drive/shared` is: there
 * is no space of theirs to put it under. Drawn by the shell's tab host, like
 * every open file.
 */
export const Route = createFileRoute("/drive/shared_/file/$nodeId")({
  head: () => ({ meta: [{ title: pageTitle(msg`Opening`) }] }),
  validateSearch: validateFileSearch,
  loader: ({ context, params }) => {
    void context.queryClient.ensureQueryData(nodeOptions(params.nodeId));
  },
});
