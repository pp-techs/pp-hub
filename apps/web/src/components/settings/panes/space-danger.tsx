import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { useDeleteSpace, useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { useActiveSpaceId } from "@/lib/use-active-space";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { LoadingStatus } from "@/components/loading";
import { FieldSkeleton } from "@/components/settings/field-skeleton";

export function SpaceDangerPane() {
  const { t } = useLingui();
  const space = useSpace(useActiveSpaceId());
  const navigate = useNavigate();
  const remove = useDeleteSpace();
  const clearLastSpaceId = useUiStore((s) => s.clearLastSpaceId);
  const [confirming, setConfirming] = useState(false);
  const [typed, setTyped] = useState("");

  if (!space)
    return (
      <LoadingStatus>
        <Card className="border-destructive/30 max-w-xl">
          <CardHeader>
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-3.5 w-full" />
            <Skeleton className="h-3.5 w-2/3" />
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <FieldSkeleton className="max-w-64" />
            <Skeleton className="h-7 w-28" />
          </CardContent>
        </Card>
      </LoadingStatus>
    );

  const spaceName = space.name;

  return (
    <Card className="border-destructive/30 max-w-xl">
      <CardHeader>
        <CardTitle className="text-destructive">
          <Trans>Delete this space</Trans>
        </CardTitle>
        <CardDescription>
          <Trans>
            Removes “{spaceName}” and everything in it — every file, version, share and member. The
            blobs are released and cannot be recovered.
          </Trans>
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="confirm-name">
            <Trans>Type the space name to confirm</Trans>
          </Label>
          <Input
            id="confirm-name"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={space.name}
            className="max-w-64"
          />
        </div>
        <div>
          <Button
            variant="destructive"
            disabled={typed !== space.name || remove.isPending}
            onClick={() => setConfirming(true)}
          >
            <Trash2 data-icon="inline-start" /> <Trans>Delete space</Trans>
          </Button>
        </div>
      </CardContent>

      {/* Inline rather than a nested better-modal, exactly as the share
          dialog does: this confirmation belongs to the pane that is already
          open, and a second entry in the modal container would outlive it. */}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t`Delete space`}
        description={t`Permanently delete "${spaceName}" and all of its contents? This cannot be undone.`}
        confirmLabel={t`Delete space`}
        // eslint-disable-next-line lingui/no-unlocalized-strings -- Button variant id
        confirmVariant="destructive"
        onConfirm={() =>
          remove.mutate(space.id, {
            onSuccess: () => {
              toast.success(t`Deleted “${spaceName}”`);
              // Otherwise /drive keeps redirecting to the space just deleted
              // and lands on its own not-found page. The navigation names no
              // hash, so it also closes the settings dialog.
              clearLastSpaceId(space.id);
              void navigate({ to: "/drive" });
            },
            onError: (e) => toast.error(t`Could not delete`, { description: e.message }),
          })
        }
      />
    </Card>
  );
}
