import { createFileRoute, redirect } from "@tanstack/react-router";
import { validateFileSearch } from "@/lib/open-file";

/**
 * Where the editor used to live, as a page of its own in a new browser tab.
 * Files open in the drive shell now; this address stays because it is in
 * people's bookmarks, browser history and old notification links, and
 * `/open/$nodeId` knows where each node belongs.
 */
export const Route = createFileRoute("/edit/$nodeId")({
  validateSearch: validateFileSearch,
  beforeLoad: ({ params, search }) => {
    throw redirect({ to: "/open/$nodeId", params, search, replace: true });
  },
});
