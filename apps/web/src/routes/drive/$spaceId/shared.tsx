import { createFileRoute } from "@tanstack/react-router";
import { Users } from "lucide-react";
import { useShared } from "@/lib/queries";
import { Surface } from "@/components/surface";
import { NodeTable } from "@/components/node-table";

export const Route = createFileRoute("/drive/$spaceId/shared")({
  head: () => ({ meta: [{ title: "Shared with me · pp-hub" }] }),
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
  const { spaceId } = Route.useParams();
  const shared = useShared(spaceId);

  return (
    <Surface>
      <div className="flex flex-col gap-0.5">
        <h2 className="font-heading text-sm font-medium">Shared with me</h2>
        <p className="text-muted-foreground text-xs">
          Everywhere your access to this space begins. Open one to see what is inside.
        </p>
      </div>
      <NodeTable
        nodes={shared.data ?? []}
        isPending={shared.isPending}
        emptyIcon={<Users className="text-muted-foreground size-10" />}
        emptyTitle="Nothing shared with you here"
        emptyDescription="When a group you are in is given a folder, or someone shares one with you, it appears here."
        allShared
      />
    </Surface>
  );
}
