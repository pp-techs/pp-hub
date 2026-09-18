import { createFileRoute } from "@tanstack/react-router";
import { Clock } from "lucide-react";
import { Surface } from "@/components/surface";
import { SpaceNodeTable } from "@/components/space-node-table";
import { useRecent } from "@/lib/queries";
import type { ActivityKind, SearchResult } from "@/lib/api";

export const Route = createFileRoute("/drive/$spaceId/recent")({
  head: () => ({ meta: [{ title: "Recent · pp-hub" }] }),
  component: RecentPage,
});

/** How the table phrases the caller's own last action on a file. */
const ACTIONS: Record<ActivityKind, string> = {
  uploaded: "You uploaded",
  created: "You created",
  renamed: "You renamed",
  moved: "You moved",
  trashed: "You trashed",
  restored: "You restored",
  purged: "You deleted",
  shared: "You shared",
  unshared: "You revoked",
  reverted: "You reverted",
  invited: "You invited",
  joined: "You joined",
};

/** Everything the caller has touched in this space. */
function RecentPage() {
  const { spaceId } = Route.useParams();
  const recent = useRecent(spaceId, 100);

  return (
    <Surface>
      <SpaceNodeTable
        nodes={recent.data ?? []}
        isPending={recent.isPending}
        emptyIcon={<Clock className="text-muted-foreground size-10" />}
        emptyTitle="Nothing recent yet"
        emptyDescription="Files you upload or edit in this space show up here."
        extraColumn={{
          header: "Action",
          cell: (n) => ACTIONS[(n as SearchResult & { last_action: ActivityKind }).last_action],
        }}
      />
    </Surface>
  );
}
