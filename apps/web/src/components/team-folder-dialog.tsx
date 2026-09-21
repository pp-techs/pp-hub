import { createModal } from "@buiducnhat/better-modal";
import { Trans, useLingui } from "@lingui/react/macro";
import { Users } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { useKeyedState } from "@/lib/use-keyed-state";

type Props = {
  spaceId: string;
  spaceName: string;
};

/**
 * Names a new team folder, and says what makes it not a normal one.
 *
 * Everywhere else in the tree the creator keeps access to what they created,
 * forever; a team folder drops that rule, so access is exactly the grants on
 * the folder and nothing else. That is the whole point — and it is invisible
 * from a name field — so the difference is spelled out before the folder
 * exists rather than discovered when someone leaves and their files go with
 * them.
 *
 * Only the name is collected: the caller owns the mutation, so the same
 * dialog serves whichever surface opened it. Creation is always at the
 * space's top level, which is why no destination is asked for.
 */
export const TeamFolderDialog = createModal<Props, string | null>(
  "new-team-folder",
  ({ spaceId, spaceName, modal }) => {
    // better-modal keeps a closed modal mounted, so a plain `useState` would
    // hand the next opening the name typed the last time. `modal.visible` is
    // part of the key so each opening starts empty, and `spaceId` so a name
    // meant for one space cannot resurface in another.
    const [name, setName] = useKeyedState(`${spaceId}:${modal.visible}`, "");
    const trimmed = name.trim();
    const { t } = useLingui();

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Users className="size-4 shrink-0" />
              <Trans>New team folder</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>
                A top-level folder in {spaceName} that nobody owns personally: access to everything
                inside it is managed through Share, so removing someone really removes them, even
                from files they uploaded. It stays with the team when whoever created it leaves.
              </Trans>
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (!trimmed) return;
              modal.resolve(trimmed);
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="team-folder-name">
                <Trans>Name</Trans>
              </Label>
              <Input
                id="team-folder-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t`Finance`}
                autoFocus
                required
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => modal.resolve(null)}>
                <Trans>Cancel</Trans>
              </Button>
              <Button type="submit" disabled={!trimmed}>
                <Trans>Create</Trans>
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    );
  },
);
