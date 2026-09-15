import { Trash2, Users } from "lucide-react";
import { useShared, useTrash } from "@/lib/queries";
import { NodeTable } from "@/components/node-table";

/**
 * Items the user soft-deleted in this space. The API exposes the listing only;
 * restore and permanent delete are not wired to endpoints yet, so the panel
 * stays read-only rather than showing buttons that cannot work.
 */
export function TrashPanel({ spaceId }: { spaceId: string }) {
  const trash = useTrash(spaceId);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className="font-heading text-sm font-medium">Trash</h2>
        <p className="text-muted-foreground text-xs">
          Items you deleted in this space. Deleted items still count toward storage.
        </p>
      </div>
      <NodeTable
        nodes={trash.data ?? []}
        isPending={trash.isPending}
        emptyIcon={<Trash2 className="text-muted-foreground size-10" />}
        emptyTitle="Trash is empty"
        emptyDescription="Items you move to trash show up here."
      />
    </div>
  );
}

/**
 * Nodes shared with the signed-in user. This listing spans every space, so it
 * is not filtered by the space currently open in the rail.
 */
export function SharedPanel() {
  const shared = useShared();

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className="font-heading text-sm font-medium">Shared with me</h2>
        <p className="text-muted-foreground text-xs">
          Files and folders other people shared with you, across all spaces.
        </p>
      </div>
      <NodeTable
        nodes={shared.data ?? []}
        isPending={shared.isPending}
        emptyIcon={<Users className="text-muted-foreground size-10" />}
        emptyTitle="Nothing shared with you yet"
        emptyDescription="When someone shares a file or folder, it appears here."
      />
    </div>
  );
}
