import { createFileRoute } from "@tanstack/react-router";
import { Clock } from "lucide-react";
import { CrossSpaceTable } from "@/components/cross-space-table";
import { useRecent } from "@/lib/queries";
import type { ActivityKind, SearchResult } from "@/lib/api";

export const Route = createFileRoute("/drive/recent")({
  head: () => ({ meta: [{ title: "Recent · pp-hub" }] }),
  component: RecentPage,
});

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

/** Everything the caller has touched, across every space. */
function RecentPage() {
  const recent = useRecent(100);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="border-border flex h-10 shrink-0 items-center border-b px-4">
        <span className="text-xs font-medium">Recent</span>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <CrossSpaceTable
          nodes={recent.data ?? []}
          isPending={recent.isPending}
          emptyIcon={<Clock className="text-muted-foreground size-10" />}
          emptyTitle="Nothing recent yet"
          emptyDescription="Files you upload or edit show up here."
          extraColumn={{
            header: "Action",
            cell: (n) => ACTIONS[(n as SearchResult & { last_action: ActivityKind }).last_action],
          }}
        />
      </div>
    </div>
  );
}
