import { createFileRoute, Link } from "@tanstack/react-router";
import { Clock, Star } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { RailLabel } from "@/components/rail-item";
import { SpaceNodeTable } from "@/components/space-node-table";
import { ActivityFeed } from "@/components/activity-feed";
import { StorageBreakdown } from "@/components/storage-breakdown";
import { ContinueCards } from "@/components/continue-cards";
import { useActivity, useRecent, useSpace, useStarred, useUsage } from "@/lib/queries";
import type { ActivityKind, SearchResult } from "@/lib/api";

export const Route = createFileRoute("/drive/$spaceId/home")({
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
 * A working dashboard for one space, not a welcome page.
 *
 * Everything on it belongs to the space in the URL — there is no all-spaces
 * mode, because a row from a space the rail is not showing cannot be opened
 * without silently switching space underneath the user.
 */
function HomePage() {
  const { spaceId } = Route.useParams();
  const space = useSpace(spaceId);
  const recent = useRecent(spaceId, 10);
  const starred = useStarred(spaceId, 10);
  const activity = useActivity(spaceId, 12);
  const usage = useUsage(spaceId);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="border-border flex h-10 shrink-0 items-center justify-between gap-2 border-b px-4">
        <div className="flex min-w-0 items-baseline gap-2">
          <span className="text-sm font-medium">Home</span>
          <span className="text-muted-foreground truncate text-[10px]">
            Everything in {space?.name ?? "this space"}
          </span>
        </div>
        <Button
          variant="outline"
          size="xs"
          render={<Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: undefined }} />}
        >
          Browse files
        </Button>
      </div>

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
            <Button
              variant="ghost"
              size="xs"
              render={<Link to="/drive/$spaceId/recent" params={{ spaceId }} />}
            >
              View all
            </Button>
          </div>
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
        </section>

        <div className="grid gap-6 lg:grid-cols-[2fr_1fr]">
          <section>
            <div className="flex items-center justify-between">
              <RailLabel>Starred</RailLabel>
              <Button
                variant="ghost"
                size="xs"
                render={<Link to="/drive/$spaceId/starred" params={{ spaceId }} />}
              >
                View all
              </Button>
            </div>
            <SpaceNodeTable
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
      </div>
    </div>
  );
}
