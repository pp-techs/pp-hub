import { createFileRoute } from "@tanstack/react-router";
import { msg } from "@lingui/core/macro";
import { Surface } from "@/components/surface";
import { TrashPanel } from "@/components/trash-panel";
import { pageTitle } from "@/lib/i18n";

export const Route = createFileRoute("/drive/$spaceId/trash")({
  head: () => ({ meta: [{ title: pageTitle(msg`Trash`) }] }),
  component: TrashPage,
});

function TrashPage() {
  const { spaceId } = Route.useParams();

  return (
    <Surface>
      <TrashPanel spaceId={spaceId} />
    </Surface>
  );
}
