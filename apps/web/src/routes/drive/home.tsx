import { createFileRoute, Link } from "@tanstack/react-router";
import { Clock, Star, Users } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { RailLabel } from "@/components/rail-item";
import { CrossSpaceTable } from "@/components/cross-space-table";
import { ActivityFeed } from "@/components/activity-feed";
import { StorageBreakdown } from "@/components/storage-breakdown";
import { ContinueCards } from "@/components/continue-cards";
import { useActivity, useRecent, useShared, useStarred, useUsage } from "@/lib/queries";
import type { ActivityKind, SearchResult } from "@/lib/api";

export const Route = createFileRoute("/drive/home")({
  head: () => ({ meta: [{ title: "Home · pp-hub" }] }),
  component: HomePage,
});

/** How the Recent table phrases the caller's own last action on a file. */
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

/**
 * A working dashboard across every space, not a welcome page.
 *
 * Reached from Home in the rail. `/drive` still redirects to the last space
 * opened, so this does not change where the app lands on sign-in — making it
 * the landing surface is a one-line change to the redirect in `drive.tsx`.
 */
function HomePage() {
  const recent = useRecent(10);
  const starred = useStarred(10);
  const shared = useShared();
  const activity = useActivity(undefined, 12);
  const usage = useUsage();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="border-border flex h-10 shrink-0 items-center border-b px-4">
        <span className="text-xs font-medium">Home</span>
      </header>

      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-auto p-4">
        {(recent.data?.length ?? 0) > 0 && (
          <section>
            <RailLabel>Continue where you left off</RailLabel>
            <ContinueCards nodes={recent.data ?? []} />
          </section>
        )}

        <section>
          <div className="flex items-center justify-between">
            <RailLabel>Recent</RailLabel>
            <Button variant="ghost" size="xs" render={<Link to="/drive/recent" />}>
              View all
            </Button>
          </div>
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
        </section>

        <div className="grid gap-6 lg:grid-cols-[2fr_1fr]">
          <section>
            <div className="flex items-center justify-between">
              <RailLabel>Starred</RailLabel>
              <Button variant="ghost" size="xs" render={<Link to="/drive/starred" />}>
                View all
              </Button>
            </div>
            <CrossSpaceTable
              nodes={starred.data ?? []}
              isPending={starred.isPending}
              emptyIcon={<Star className="text-muted-foreground size-10" />}
              emptyTitle="Nothing starred"
              emptyDescription="Star a file from its ⋯ menu to keep it here."
            />
          </section>

          <div className="flex flex-col gap-6">
            <section>
              <RailLabel>Storage</RailLabel>
              {usage.isPending ? (
                <p className="text-muted-foreground text-xs">Loading…</p>
              ) : (
                <StorageBreakdown usage={usage.data ?? []} />
              )}
            </section>

            <section>
              <RailLabel>Activity</RailLabel>
              {activity.isPending ? (
                <p className="text-muted-foreground text-xs">Loading…</p>
              ) : (
                <ActivityFeed events={activity.data ?? []} />
              )}
            </section>
          </div>
        </div>

        <section>
          <RailLabel>Shared with me</RailLabel>
          <CrossSpaceTable
            nodes={(shared.data ?? []) as SearchResult[]}
            isPending={shared.isPending}
            emptyIcon={<Users className="text-muted-foreground size-10" />}
            emptyTitle="Nothing shared with you yet"
            emptyDescription="When someone shares a file or folder, it appears here."
          />
        </section>
      </div>
    </div>
  );
}
