import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { useDeleteSpace, useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { pageTitle } from "@/lib/i18n";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Surface } from "@/components/surface";

export const Route = createFileRoute("/drive/$spaceId/settings/danger")({
  head: () => ({ meta: [{ title: pageTitle(msg`Danger zone`, msg`Space settings`) }] }),
  component: DangerPage,
});

function DangerPage() {
  const { t } = useLingui();
  const { spaceId } = Route.useParams();
  const space = useSpace(spaceId);
  const navigate = useNavigate();
  const remove = useDeleteSpace();
  const clearLastSpaceId = useUiStore((s) => s.clearLastSpaceId);
  const [confirming, setConfirming] = useState(false);
  const [typed, setTyped] = useState("");

  if (!space)
    return (
      <p className="text-muted-foreground p-4 text-xs">
        <Trans>Loading…</Trans>
      </p>
    );

  const spaceName = space.name;

  return (
    <Surface className="max-w-xl">
      <Card className="border-destructive/30">
        <CardHeader>
          <CardTitle className="text-destructive">
            <Trans>Delete this space</Trans>
          </CardTitle>
          <CardDescription>
            <Trans>
              Removes “{spaceName}” and everything in it — every file, version, share and member.
              The blobs are released and cannot be recovered.
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
                // and lands on its own not-found page.
                clearLastSpaceId(space.id);
                void navigate({ to: "/drive" });
              },
              onError: (e) => toast.error(t`Could not delete`, { description: e.message }),
            })
          }
        />
      </Card>
    </Surface>
  );
}
