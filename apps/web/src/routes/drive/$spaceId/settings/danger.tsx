import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { useDeleteSpace, useSpace } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import { ConfirmDialog } from "@/components/confirm-dialog";

export const Route = createFileRoute("/drive/$spaceId/settings/danger")({
  head: () => ({ meta: [{ title: "Danger zone · Space settings · pp-hub" }] }),
  component: DangerPage,
});

function DangerPage() {
  const { spaceId } = Route.useParams();
  const space = useSpace(spaceId);
  const navigate = useNavigate();
  const remove = useDeleteSpace();
  const clearLastSpaceId = useUiStore((s) => s.clearLastSpaceId);
  const [confirming, setConfirming] = useState(false);
  const [typed, setTyped] = useState("");

  if (!space) return <p className="text-muted-foreground p-4 text-xs">Loading…</p>;

  return (
    <div className="overflow-y-auto p-4">
      <div className="border-destructive/30 flex max-w-xl flex-col gap-2 rounded-lg border p-3">
        <h2 className="text-destructive text-xs font-medium">Delete this space</h2>
        <p className="text-muted-foreground text-xs">
          Removes “{space.name}” and everything in it — every file, version, share and member. The
          blobs are released and cannot be recovered.
        </p>
        <div className="flex flex-col gap-1">
          <Label htmlFor="confirm-name">Type the space name to confirm</Label>
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
            <Trash2 data-icon="inline-start" /> Delete space
          </Button>
        </div>

        <ConfirmDialog
          open={confirming}
          onOpenChange={setConfirming}
          title="Delete space"
          description={`Permanently delete "${space.name}" and all of its contents? This cannot be undone.`}
          confirmLabel="Delete space"
          confirmVariant="destructive"
          onConfirm={() =>
            remove.mutate(space.id, {
              onSuccess: () => {
                toast.success(`Deleted “${space.name}”`);
                // Otherwise /drive keeps redirecting to the space just deleted
                // and lands on its own not-found page.
                clearLastSpaceId(space.id);
                void navigate({ to: "/drive" });
              },
              onError: (e) => toast.error("Could not delete", { description: e.message }),
            })
          }
        />
      </div>
    </div>
  );
}
