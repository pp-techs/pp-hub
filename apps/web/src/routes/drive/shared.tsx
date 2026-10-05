import { createFileRoute } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { Download, Globe } from "lucide-react";
import { buttonVariants } from "@workspace/ui/components/button";
import { capsOf } from "@/lib/api";
import { useExternalShared, useNodeChildren } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { Surface } from "@/components/surface";
import { NodeTable } from "@/components/node-table";
import { downloadUrl } from "@/components/node-actions";
import { useOpenNode } from "@/components/use-open-node";

export const Route = createFileRoute("/drive/shared")({
  head: () => ({ meta: [{ title: pageTitle(msg`Shared from outside`) }] }),
  validateSearch: (search: Record<string, unknown>): { dir?: string } => ({
    dir: typeof search.dir === "string" ? search.dir : undefined,
  }),
  component: ExternalSharedPage,
});

/**
 * What people in spaces the caller does not belong to have shared with them.
 * The root lists those entry points with the space each lives in; `?dir=`
 * browses one shared folder by node id, the only listing an outsider may use.
 */
function ExternalSharedPage() {
  const { t } = useLingui();
  const { dir } = Route.useSearch();
  const roots = useExternalShared();
  const children = useNodeChildren(dir ?? null);
  const listing = dir ? children : roots;
  const nodes = listing.data ?? [];
  const openNode = useOpenNode();

  return (
    <Surface>
      {!dir && (
        <div className="flex flex-col gap-0.5">
          <h2 className="font-heading text-sm font-medium">
            <Trans>Shared from outside</Trans>
          </h2>
          <p className="text-muted-foreground text-xs">
            <Trans>Files and folders people in other spaces have shared with you.</Trans>
          </p>
        </div>
      )}
      {listing.isError ? (
        <p className="text-muted-foreground py-12 text-center text-xs">
          <Trans>This folder is not available. It may have been unshared or deleted.</Trans>
        </p>
      ) : (
        <NodeTable
          nodes={nodes}
          isPending={listing.isPending}
          emptyIcon={<Globe className="text-muted-foreground size-10" />}
          emptyTitle={dir ? t`This folder is empty` : t`Nothing shared with you from outside`}
          emptyDescription={
            dir
              ? t`Nothing has been added here yet.`
              : t`When someone outside your spaces shares a file or folder with you, it appears here.`
          }
          allShared={!dir}
          onOpen={(node) => openNode(node, nodes)}
          extraColumn={
            dir ? undefined : { header: t`Space`, cell: (node) => node.space_name ?? "—" }
          }
          renderActions={(node) =>
            node.kind === "file" && capsOf(node).download ? (
              <a
                href={downloadUrl(node)}
                download
                aria-label={t`Download`}
                className={buttonVariants({ variant: "ghost", size: "icon-xs" })}
              >
                <Download />
              </a>
            ) : null
          }
        />
      )}
    </Surface>
  );
}
