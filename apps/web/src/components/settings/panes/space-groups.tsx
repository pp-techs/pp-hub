import { useState } from "react";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { useSpace } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { LoadingStatus } from "@/components/loading";
import { GroupList } from "@/components/group-list";

/**
 * The space's groups, and who is in the one being inspected.
 *
 * A pane of its own rather than a column beside the member table: the two
 * answer different questions — "who is in this space" and "what does this
 * group open" — and a 20rem drawer has nowhere to sit inside a dialog.
 */
export function SpaceGroupsPane() {
  const space = useSpace(useActiveSpaceId());
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);

  if (!space)
    return (
      <LoadingStatus className="flex max-w-3xl flex-col gap-3">
        <Skeleton className="h-2.5 w-2/3" />
        <div className="flex flex-col gap-0.5">
          {[30, 45, 38, 52].map((width) => (
            <div key={width} className="flex items-center gap-2 px-2 py-1.5">
              <Skeleton className="h-3" style={{ width: `${width}%` }} />
              <Skeleton className="ml-auto h-3 w-12" />
            </div>
          ))}
        </div>
      </LoadingStatus>
    );

  return <GroupList space={space} selectedId={selectedId} onSelect={setSelectedId} />;
}
