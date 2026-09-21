import { createFileRoute, Link } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { MessageDescriptor } from "@lingui/core";
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
import { pageTitle } from "@/lib/i18n";
import type { ActivityKind, SearchResult } from "@/lib/api";

export const Route = createFileRoute("/drive/$spaceId/home")({
  head: () => ({ meta: [{ title: pageTitle(msg`Home`) }] }),
  component: HomePage,
});

/**
 * How the Recent table phrases the caller's own last action on a file.
 *
 * Descriptors, not strings: this is module scope and runs before a catalog is
 * active, so a rendered phrase here would pin whichever language loaded first
 * and never follow a language switch. `i18n._()` at render time is what makes
 * it reactive.
 */
const ACTIONS: Record<ActivityKind, MessageDescriptor> = {
  uploaded: msg`You uploaded`,
  created: msg`You created`,
  renamed: msg`You renamed`,
  moved: msg`You moved`,
  trashed: msg`You trashed`,
  restored: msg`You restored`,
  purged: msg`You deleted`,
  shared: msg`You shared`,
  unshared: msg`You revoked`,
  reverted: msg`You reverted`,
  invited: msg`You invited`,
  joined: msg`You joined`,
};

/**
 * A working dashboard for one space, not a welcome page.
 *
 * Everything on it belongs to the space in the URL — there is no all-spaces
 * mode, because a row from a space the rail is not showing cannot be opened
 * without silently switching space underneath the user.
 */
function HomePage() {
  const { t, i18n } = useLingui();
  const { spaceId } = Route.useParams();
  const space = useSpace(spaceId);
  const recent = useRecent(spaceId, 10);
  const starred = useStarred(spaceId, 10);
  const activity = useActivity(spaceId, 12);
  const usage = useUsage(spaceId);
  const spaceName = space?.name ?? t`this space`;

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
          <Trans>Browse files</Trans>
        </Button>
      </HeaderActions>

      <p className="text-muted-foreground -mt-1 text-xs">
        <Trans>Everything in {spaceName}</Trans>
      </p>

      {(recent.data?.length ?? 0) > 0 && (
        <section>
          <SectionLabel>
            <Trans>Continue where you left off</Trans>
          </SectionLabel>
          <ContinueCards nodes={recent.data ?? []} />
        </section>
      )}

      <div className="grid gap-4 md:gap-6 @4xl/main:grid-cols-[2fr_1fr]">
        <div className="flex flex-col gap-4 md:gap-6">
          <section>
            <div className="flex items-center justify-between">
              <SectionLabel>
                <Trans>Recent</Trans>
              </SectionLabel>
              <Button
                variant="ghost"
                size="xs"
                render={<Link to="/drive/$spaceId/recent" params={{ spaceId }} />}
              >
                <Trans>View all</Trans>
              </Button>
            </div>
            <SpaceNodeTable
              nodes={recent.data ?? []}
              isPending={recent.isPending}
              emptyIcon={<Clock className="text-muted-foreground size-10" />}
              emptyTitle={t`Nothing recent yet`}
              emptyDescription={t`Files you upload or edit in this space show up here.`}
              extraColumn={{
                header: t`Action`,
                cell: (n) => {
                  const action =
                    ACTIONS[(n as SearchResult & { last_action: ActivityKind }).last_action];
                  return action ? i18n._(action) : null;
                },
              }}
            />
          </section>

          <section>
            <div className="flex items-center justify-between">
              <SectionLabel>
                <Trans>Starred</Trans>
              </SectionLabel>
              <Button
                variant="ghost"
                size="xs"
                render={<Link to="/drive/$spaceId/starred" params={{ spaceId }} />}
              >
                <Trans>View all</Trans>
              </Button>
            </div>
            <SpaceNodeTable
              nodes={starred.data ?? []}
              isPending={starred.isPending}
              emptyIcon={<Star className="text-muted-foreground size-10" />}
              emptyTitle={t`Nothing starred`}
              emptyDescription={t`Star a file from its ⋯ menu to keep it here.`}
            />
          </section>
        </div>

        <div className="flex flex-col gap-4 md:gap-6">
          <section>
            <SectionLabel>
              <Trans>Storage</Trans>
            </SectionLabel>
            {usage.isPending ? (
              <p className="text-muted-foreground text-xs">
                <Trans>Loading…</Trans>
              </p>
            ) : (
              <StorageBreakdown usage={usage.data ?? []} />
            )}
          </section>

          <section>
            <SectionLabel>
              <Trans>Activity</Trans>
            </SectionLabel>
            {activity.isPending ? (
              <p className="text-muted-foreground text-xs">
                <Trans>Loading…</Trans>
              </p>
            ) : (
              <ActivityFeed events={activity.data ?? []} />
            )}
          </section>
        </div>
      </div>
    </Surface>
  );
}
