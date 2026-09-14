import { createFileRoute } from "@tanstack/react-router";
import { SpaceMembersPanel } from "@/components/space-members-panel";
import { spacesOptions } from "@/lib/queries";

export const Route = createFileRoute("/drive/$spaceId/settings")({
  head: () => ({ meta: [{ title: "Space Settings · pp-hub" }] }),
  component: SpaceSettings,
});

function SpaceSettings() {
  const { spaceId } = Route.useParams();
  const spaces = spacesOptions();
  const currentUserRole = spaces.data?.find((s) => s.id === spaceId)?.role ?? "viewer";

  return (
    <div className="flex min-h-0 flex-1 flex-col p-6">
      <SpaceMembersPanel spaceId={spaceId} currentUserRole={currentUserRole} />
    </div>
  );
}
