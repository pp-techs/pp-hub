import { createFileRoute, Link } from "@tanstack/react-router";
import { Clock, Star } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { HeaderActions } from "@/components/header-slot";
import { SectionLabel } from "@/components/section-label";
import { Surface } from "@/components/surface";
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
    <Surface>
      {/* The shell's header names this surface; the one thing it cannot know
          is where "Browse files" goes, so the route puts it there itself. */}
      <HeaderActions>
        <Button
          variant="outline"
          size="xs"
          render={<Link to="/drive/$spaceId" params={{ spaceId }} search={{ dir: undefined }} />}
        >
          Browse files
        </Button>
      </HeaderActions>

      <p className="text-muted-foreground -mt-1 text-xs">
        Everything in {space?.name ?? "this space"}
      </p>

      {(recent.data?.length ?? 0) > 0 && (
        <section>
          <SectionLabel>Continue where you left off</SectionLabel>
          <ContinueCards nodes={recent.data ?? []} />
        </section>
      )}

      <div className="grid gap-4 md:gap-6 @4xl/main:grid-cols-[2fr_1fr]">
        <div className="flex flex-col gap-4 md:gap-6">
          <section>
            <div className="flex items-center justify-between">
              <SectionLabel>Recent</SectionLabel>
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
                cell: (n) =>
                  ACTIONS[(n as SearchResult & { last_action: ActivityKind }).last_action],
              }}
            />
          </section>

          <section>
            <div className="flex items-center justify-between">
              <SectionLabel>Starred</SectionLabel>
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
        </div>

        <div className="flex flex-col gap-4 md:gap-6">
          <section>
            <SectionLabel>Storage</SectionLabel>
            {usage.isPending ? (
              <p className="text-muted-foreground text-xs">Loading…</p>
            ) : (
              <StorageBreakdown usage={usage.data ?? []} />
            )}
          </section>

          <section>
            <SectionLabel>Activity</SectionLabel>
            {activity.isPending ? (
              <p className="text-muted-foreground text-xs">Loading…</p>
            ) : (
              <ActivityFeed events={activity.data ?? []} />
            )}
          </section>
        </div>
      </div>
    </Surface>
  );
}
