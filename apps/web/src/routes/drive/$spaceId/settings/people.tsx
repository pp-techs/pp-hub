import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Trans } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { useSpace } from "@/lib/queries";
import { pageTitle } from "@/lib/i18n";
import { Surface } from "@/components/surface";
import { PeopleTable } from "@/components/people-table";
import { GroupDrawer } from "@/components/group-drawer";

export const Route = createFileRoute("/drive/$spaceId/settings/people")({
  head: () => ({ meta: [{ title: pageTitle(msg`People`, msg`Space settings`) }] }),
  validateSearch: (search: Record<string, unknown>): { group?: string } => ({
    group: typeof search.group === "string" ? search.group : undefined,
  }),
  component: PeoplePage,
});

/**
 * The only place space membership is managed: who is in, who administers it,
 * and which groups they belong to — plus invitations that have not been taken
 * up yet.
 *
 * The selected group is a search param so the pairing of "these people" and
 * "this group" is one shareable address, and so the table filter and the
 * drawer cannot disagree about which group is open.
 */
function PeoplePage() {
  const { spaceId } = Route.useParams();
  const { group } = Route.useSearch();
  const navigate = useNavigate();
  const space = useSpace(spaceId);

  const selectGroup = (next: string | undefined) =>
    void navigate({
      to: "/drive/$spaceId/settings/people",
      params: { spaceId },
      search: { group: next },
      replace: true,
    });

  if (!space)
    return (
      <p className="text-muted-foreground p-4 text-xs">
        <Trans>Loading…</Trans>
      </p>
    );

  return (
    <div className="flex min-h-0 flex-1">
      <Surface>
        <PeopleTable space={space} groupFilter={group} onGroupFilterChange={selectGroup} />
      </Surface>
      <aside className="border-border w-80 shrink-0 border-l">
        <GroupDrawer space={space} selectedId={group} onSelect={selectGroup} />
      </aside>
    </div>
  );
}
