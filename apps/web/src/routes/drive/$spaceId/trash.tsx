import { createFileRoute } from "@tanstack/react-router";
import { Surface } from "@/components/surface";
import { TrashPanel } from "@/components/trash-panel";

export const Route = createFileRoute("/drive/$spaceId/trash")({
  head: () => ({ meta: [{ title: "Trash · pp-hub" }] }),
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
