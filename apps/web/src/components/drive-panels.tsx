import { Users } from "lucide-react";
import { useShared } from "@/lib/queries";
import { NodeTable } from "@/components/node-table";

/**
 * Where the signed-in user's access starts, across every space — so it is not
 * filtered by the space open in the rail.
 *
 * Each row is one grant: a folder a group they belong to was given, or one
 * shared with them directly. A folder sitting under another granted folder is
 * left out, since walking down from the parent already reaches it.
 */
export function SharedPanel() {
  const shared = useShared();

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className="font-heading text-sm font-medium">Shared with me</h2>
        <p className="text-muted-foreground text-xs">
          Everywhere your access begins, across every space. Open one to see what is inside.
        </p>
      </div>
      <NodeTable
        nodes={shared.data ?? []}
        isPending={shared.isPending}
        emptyIcon={<Users className="text-muted-foreground size-10" />}
        emptyTitle="Nothing shared with you yet"
        emptyDescription="When a group you are in is given a folder, or someone shares one with you, it appears here."
      />
    </div>
  );
}
