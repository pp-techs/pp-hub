import { useState } from "react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { useSpace } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { LoadingStatus } from "@/components/loading";
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
      <LoadingStatus className="flex flex-col gap-3">
        <div className="flex items-start justify-between gap-2">
          <div className="flex flex-col gap-1.5">
            <Skeleton className="h-4 w-20" />
            <Skeleton className="h-2.5 w-32" />
          </div>
          <Skeleton className="h-7 w-28" />
        </div>
        <Skeleton className="h-7 w-full max-w-64" />
        <div className="flex flex-col">
          {[40, 55, 35, 48, 60].map((width) => (
            <div key={width} className="border-border flex h-10 items-center border-b px-2">
              <Skeleton className="h-3.5" style={{ width: `${width}%` }} />
            </div>
          ))}
        </div>
      </LoadingStatus>
    );

  return (
    <PeopleTable space={space} groupFilter={groupFilter} onGroupFilterChange={setGroupFilter} />
  );
}
