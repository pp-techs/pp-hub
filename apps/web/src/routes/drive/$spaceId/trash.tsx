import { createFileRoute } from "@tanstack/react-router";
import { TrashPanel } from "@/components/trash-panel";

export const Route = createFileRoute("/drive/$spaceId/trash")({
  head: () => ({ meta: [{ title: "Trash · pp-hub" }] }),
  component: TrashPage,
});

function TrashPage() {
  const { spaceId } = Route.useParams();

  return (
    <div className="min-h-0 flex-1 overflow-auto p-4">
      <TrashPanel spaceId={spaceId} />
    </div>
  );
}
