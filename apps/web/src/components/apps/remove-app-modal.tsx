import { useState } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { createModal } from "@buiducnhat/better-modal";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@workspace/ui/components/alert-dialog";
import { Button } from "@workspace/ui/components/button";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Label } from "@workspace/ui/components/label";
import { useRemoveApp } from "@/lib/queries";

interface RemoveAppProps extends Record<string, unknown> {
  extId: string;
  name: string;
}

/**
 * Uninstalls an uploaded app (M51). Its items stay in the drive either way;
 * without "delete their data" a reinstall of the same app brings them back
 * whole.
 */
export const RemoveAppModal = createModal<RemoveAppProps>(
  "remove-app",
  ({ extId, name, modal }) => {
    const { t } = useLingui();
    const remove = useRemoveApp();
    const [purge, setPurge] = useState(false);

    const close = () => {
      setPurge(false);
      modal.resolve(undefined);
    };

    const confirm = () =>
      remove.mutate(
        { extId, purge },
        {
          onSuccess: () => {
            toast.success(t`Removed ${name}`);
            close();
          },
          onError: (e) => toast.error(t`Could not remove ${name}`, { description: e.message }),
        },
      );

    return (
      <AlertDialog open={modal.visible} onOpenChange={(open) => !open && close()}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              <Trans>Remove {name}?</Trans>
            </AlertDialogTitle>
            <AlertDialogDescription>
              <Trans>
                Nobody can open its items any more. The items stay in the drive, marked as belonging
                to an app this install does not have; uploading the app again brings them back.
              </Trans>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-start gap-2">
            <Checkbox
              id="remove-app-purge"
              checked={purge}
              onCheckedChange={(v) => setPurge(v === true)}
            />
            <Label
              htmlFor="remove-app-purge"
              className="text-destructive text-xs/relaxed font-normal"
            >
              <Trans>Also delete everything stored in its items. This cannot be undone.</Trans>
            </Label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>
              <Trans>Cancel</Trans>
            </AlertDialogCancel>
            <Button variant="destructive" onClick={confirm} disabled={remove.isPending}>
              <Trans>Remove app</Trans>
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    );
  },
);
