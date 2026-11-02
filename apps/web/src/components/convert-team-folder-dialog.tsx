import { createModal } from "@buiducnhat/better-modal";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { Users } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import type { Node } from "@/lib/api";
import { useConvertToTeamFolder, useNode } from "@/lib/queries";

type Props = {
  node: Node;
};

/**
 * Asks before turning a folder the caller owns into a team folder.
 *
 * The folder keeps its place and its id — every link into it still works — but
 * the rule about who owns what inside it changes, and that is invisible from a
 * menu item. Anyone who created something in it stops owning it and keeps
 * access only through a grant, so the dialog says how many such items there
 * are (`foreign_owned`, resolved by the single-node GET only) before the
 * caller commits. The server enforces who may do this; the menu only offers it
 * to those it will accept.
 *
 * Owns its mutation, like the share dialog: the menu entry that opens it is a
 * plain function with no hooks to run one from.
 */
export const ConvertTeamFolderDialog = createModal<Props, boolean>(
  "convert-team-folder",
  ({ node, modal }) => {
    const { t } = useLingui();
    const convert = useConvertToTeamFolder(node.space_id);
    // Not the listing's copy: only the single-node GET counts what others made.
    const detail = useNode(modal.visible ? node.id : undefined);
    const others = detail.data?.foreign_owned ?? 0;
    const folderName = node.name;

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(false)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Users className="size-4 shrink-0" />
              <Trans>Convert to team folder</Trans>
            </DialogTitle>
            <DialogDescription>
              <Trans>
                “{folderName}” stays where it is and every link to it keeps working. It becomes a
                folder nobody owns personally: access to everything inside it is managed through
                Share, so removing someone really removes them, even from files they uploaded. It
                stays with the team when whoever created it leaves.
              </Trans>
            </DialogDescription>
          </DialogHeader>

          <div className="text-muted-foreground flex flex-col gap-2 text-sm">
            {detail.isError && (
              <p className="text-destructive">
                <Trans>
                  Could not check who else has contributed to this folder, so it cannot be converted
                  yet. Close this and try again.
                </Trans>
              </p>
            )}
            <p>
              <Trans>
                You and whoever created the folder become its managers. Everyone else keeps only the
                access they were given through Share.
              </Trans>
            </p>
            {others > 0 && (
              <p className="text-foreground">
                <Plural
                  value={others}
                  one="# item inside was created by somebody else. They stop owning it and keep access only through Share."
                  other="# items inside were created by somebody else. They stop owning them and keep access only through Share."
                />
              </p>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => modal.resolve(false)}>
              <Trans>Cancel</Trans>
            </Button>
            <Button
              type="button"
              // Only once the count of other people's items is known: it is the
              // one consequence the confirmation exists to show.
              disabled={convert.isPending || !detail.isSuccess}
              onClick={() =>
                convert.mutate(node.id, {
                  onSuccess: () => {
                    toast.success(t`“${folderName}” is now a team folder`);
                    modal.resolve(true);
                  },
                  onError: (e) =>
                    toast.error(t`Could not convert to a team folder`, { description: e.message }),
                })
              }
            >
              <Trans>Convert</Trans>
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);
