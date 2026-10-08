import { createModal } from "@buiducnhat/better-modal";
import { Trans, useLingui } from "@lingui/react/macro";
import { Puzzle } from "lucide-react";
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
import { ExtIconTile } from "@/components/ext-icon-tile";
import { extKindOf } from "@/ext/registry";
import { useKeyedState } from "@/lib/use-keyed-state";

type Props = {
  /** The node's `ext_kind`, e.g. `kanban.board`. */
  extKind: string;
};

/**
 * Names a new extension node (a kanban board). The title, icon and starting
 * name come from the kind's registration; the caller owns the mutation, as
 * with the other creation dialogs, and gets the trimmed name or null.
 */
export const NewExtNodeDialog = createModal<Props, string | null>(
  "new-ext-node",
  ({ extKind, modal }) => {
    const { i18n } = useLingui();
    const kind = extKindOf(extKind);
    const Icon = kind?.icon ?? Puzzle;
    const defaultName = kind ? i18n._(kind.defaultName) : "";
    // Each opening starts from the default name, never the last one typed.
    const [name, setName] = useKeyedState(`${extKind}:${modal.visible}`, defaultName);
    const trimmed = name.trim();

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader className="flex-row items-center gap-3">
            <ExtIconTile icon={Icon} />
            <div className="flex min-w-0 flex-col gap-0.5">
              <DialogTitle>{kind ? i18n._(kind.newLabel) : null}</DialogTitle>
              <DialogDescription>
                <Trans>Opens it in the editor when it is created.</Trans>
              </DialogDescription>
            </div>
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
              <Label
                htmlFor="new-ext-node-name"
                className="text-muted-foreground text-[10px] font-medium tracking-wider uppercase"
              >
                <Trans>Name</Trans>
              </Label>
              <Input
                id="new-ext-node-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
                required
                onFocus={(e) => e.currentTarget.select()}
              />
              {kind?.newHint ? (
                <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
                  <span aria-hidden className="bg-primary size-1.5 shrink-0 rounded-full" />
                  {i18n._(kind.newHint)}
                </p>
              ) : null}
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
