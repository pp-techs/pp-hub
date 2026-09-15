import { createFileRoute } from "@tanstack/react-router";
import { Star } from "lucide-react";
import { CrossSpaceTable } from "@/components/cross-space-table";
import { useStarred } from "@/lib/queries";

export const Route = createFileRoute("/drive/starred")({
  head: () => ({ meta: [{ title: "Starred · pp-hub" }] }),
  component: StarredPage,
});

/** The caller's bookmarks, across every space. */
function StarredPage() {
  const starred = useStarred(100);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="border-border flex h-10 shrink-0 items-center border-b px-4">
        <span className="text-xs font-medium">Starred</span>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        <CrossSpaceTable
          nodes={starred.data ?? []}
          isPending={starred.isPending}
          emptyIcon={<Star className="text-muted-foreground size-10" />}
          emptyTitle="Nothing starred"
          emptyDescription="Star a file from its ⋯ menu to keep it here."
        />
      </div>
    </div>
  );
}
