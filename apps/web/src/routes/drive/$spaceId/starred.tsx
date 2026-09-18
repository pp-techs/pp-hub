import { createFileRoute } from "@tanstack/react-router";
import { Star } from "lucide-react";
import { Surface } from "@/components/surface";
import { SpaceNodeTable } from "@/components/space-node-table";
import { useStarred } from "@/lib/queries";

export const Route = createFileRoute("/drive/$spaceId/starred")({
  head: () => ({ meta: [{ title: "Starred · pp-hub" }] }),
  component: StarredPage,
});

/** The caller's bookmarks in this space. */
function StarredPage() {
  const { spaceId } = Route.useParams();
  const starred = useStarred(spaceId, 100);

  return (
    <Surface>
      <SpaceNodeTable
        nodes={starred.data ?? []}
        isPending={starred.isPending}
        emptyIcon={<Star className="text-muted-foreground size-10" />}
        emptyTitle="Nothing starred"
        emptyDescription="Star a file from its ⋯ menu to keep it here."
      />
    </Surface>
  );
}
