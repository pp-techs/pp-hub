import { Link, createFileRoute, redirect } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { ShieldX } from "lucide-react";
import { buttonVariants } from "@workspace/ui/components/button";
import { ApiError, opensInEditor } from "@/lib/api";
import { fetchSpaces, nodeOptions, requireAuth } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";

/**
 * The address mailed to people a node is shared with. It resolves to wherever
 * that node is best opened for this account: inside their own space when they
 * are a member of it, through the outside-shares browser when not, straight
 * into the editor for a file the editor opens.
 */
export const Route = createFileRoute("/open/$nodeId")({
  head: () => ({ meta: [{ title: pageTitle(msg`Opening`) }] }),
  beforeLoad: async ({ context, location }) => {
    await requireAuth(context.queryClient, location.href);
  },
  loader: async ({ context: { queryClient }, params: { nodeId } }) => {
    const [node, spaces] = await Promise.all([
      queryClient.fetchQuery(nodeOptions(nodeId)).catch((err: unknown) => {
        if (err instanceof ApiError && (err.status === 403 || err.status === 404)) return null;
        throw err;
      }),
      fetchSpaces(queryClient),
    ]);
    // Every reachable node redirects below; only a refused one renders.
    if (!node) return;

    if (node.kind === "file" && opensInEditor(node)) {
      throw redirect({ to: "/edit/$nodeId", params: { nodeId: node.id }, search: {} });
    }
    if (spaces.some((s) => s.id === node.space_id)) {
      throw redirect({
        to: "/drive/$spaceId",
        params: { spaceId: node.space_id },
        search: { dir: node.kind === "dir" ? node.id : node.parent_id },
      });
    }
    if (node.kind === "dir") {
      throw redirect({ to: "/drive/shared", search: { dir: node.id } });
    }
    // A file shared on its own: its folder may be out of reach, in which case
    // the entry-point list is where the file shows up.
    const parentViewable =
      node.parent_id !== undefined &&
      (await queryClient
        .fetchQuery(nodeOptions(node.parent_id))
        .then(() => true)
        .catch(() => false));
    throw redirect({
      to: "/drive/shared",
      search: { dir: parentViewable ? node.parent_id : undefined },
    });
  },
  component: NoAccess,
});

function NoAccess() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-3 p-8">
      <ShieldX className="text-muted-foreground size-12" />
      <h1 className="text-sm font-medium">
        <Trans>You do not have access to this item</Trans>
      </h1>
      <p className="text-muted-foreground max-w-sm text-center text-xs">
        <Trans>
          It may have been deleted or unshared, or it was shared with a different account. Ask the
          person who sent the link to share it with you again.
        </Trans>
      </p>
      <Link to="/drive" className={buttonVariants({ variant: "outline" })}>
        <Trans>Back to Drive</Trans>
      </Link>
    </div>
  );
}
