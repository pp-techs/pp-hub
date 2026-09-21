import { useState } from "react";
import { Trans } from "@lingui/react/macro";
import { useSpace } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
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
      <p className="text-muted-foreground text-xs">
        <Trans>Loading…</Trans>
      </p>
    );

  return <GroupList space={space} selectedId={selectedId} onSelect={setSelectedId} />;
}
