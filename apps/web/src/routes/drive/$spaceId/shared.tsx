import { createFileRoute } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { Users } from "lucide-react";
import { useShared } from "@/lib/queries";
import { Surface } from "@/components/surface";
import { NodeTable } from "@/components/node-table";
import { pageTitle } from "@/lib/i18n";

export const Route = createFileRoute("/drive/$spaceId/shared")({
  head: () => ({ meta: [{ title: pageTitle(msg`Shared with me`) }] }),
  component: SharedPage,
});

/**
 * Where the caller's access to this space starts.
 *
 * Each row is one grant: a folder a group they belong to was given, or one
 * shared with them directly. A folder sitting under another granted folder is
 * left out, since walking down from the parent already reaches it — which is
 * also why the rows need no location column, they *are* the entry points.
 */
function SharedPage() {
  const { t } = useLingui();
  const { spaceId } = Route.useParams();
  const shared = useShared(spaceId);

  return (
    <Surface>
      <div className="flex flex-col gap-0.5">
        <h2 className="font-heading text-sm font-medium">
          <Trans>Shared with me</Trans>
        </h2>
        <p className="text-muted-foreground text-xs">
          <Trans>
            Everywhere your access to this space begins. Open one to see what is inside.
          </Trans>
        </p>
      </div>
      <NodeTable
        nodes={shared.data ?? []}
        isPending={shared.isPending}
        emptyIcon={<Users className="text-muted-foreground size-10" />}
        emptyTitle={t`Nothing shared with you here`}
        emptyDescription={t`When a group you are in is given a folder, or someone shares one with you, it appears here.`}
        allShared
      />
    </Surface>
  );
}
