import { useState } from "react";
import { Trans } from "@lingui/react/macro";
import { useSpace } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { PeopleTable } from "@/components/people-table";

/**
 * Everyone in the space, and what they are to it.
 *
 * The group filter is plain component state rather than the search param it
 * used to be. A filter is only worth an address when the address can be
 * shared, and the dialog's own address already names the pane; a filtered
 * table inside it is a glance, not a place. Managing the groups themselves
 * is the neighbouring pane.
 */
export function SpaceMembersPane() {
  const space = useSpace(useActiveSpaceId());
  const [groupFilter, setGroupFilter] = useState<string | undefined>(undefined);

  if (!space)
    return (
      <p className="text-muted-foreground text-xs">
        <Trans>Loading…</Trans>
      </p>
    );

  return (
    <PeopleTable space={space} groupFilter={groupFilter} onGroupFilterChange={setGroupFilter} />
  );
}
