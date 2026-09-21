import { createFileRoute } from "@tanstack/react-router";
import { useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { Star } from "lucide-react";
import { Surface } from "@/components/surface";
import { SpaceNodeTable } from "@/components/space-node-table";
import { useStarred } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";

export const Route = createFileRoute("/drive/$spaceId/starred")({
  head: () => ({ meta: [{ title: pageTitle(msg`Starred`) }] }),
  component: StarredPage,
});

/** The caller's bookmarks in this space. */
function StarredPage() {
  const { t } = useLingui();
  const { spaceId } = Route.useParams();
  const starred = useStarred(spaceId, 100);

  return (
    <Surface>
      <SpaceNodeTable
        nodes={starred.data ?? []}
        isPending={starred.isPending}
        emptyIcon={<Star className="text-muted-foreground size-10" />}
        emptyTitle={t`Nothing starred`}
        emptyDescription={t`Star a file from its ⋯ menu to keep it here.`}
      />
    </Surface>
  );
}
