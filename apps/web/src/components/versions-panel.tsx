import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { fmtSize, type Node } from "@/lib/api";
import { useRestoreVersion, versionsOptions } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";

export function VersionsPanel({ node }: { node: Node }) {
  const close = useUiStore((s) => s.closePanel);
  const versions = versionsOptions(node.id);
  const restore = useRestoreVersion(node.space_id);
  const [busy, setBusy] = useState<string | null>(null);

  return (
    <Dialog open onOpenChange={(v) => !v && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Versions of “{node.name}”</DialogTitle>
          <DialogDescription>
            Restoring creates a new version with the old content.
          </DialogDescription>
        </DialogHeader>
        {versions.isPending ? (
          <p className="text-muted-foreground text-sm">Loading…</p>
        ) : versions.isError ? (
          <p className="text-destructive text-sm">{(versions.error as Error).message}</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {versions.data.map((v) => (
              <li key={v.id} className="flex items-center gap-3 text-sm">
                <span className="font-medium">v{v.version_no}</span>
                <span className="text-muted-foreground">{fmtSize(v.size)}</span>
                <span className="text-muted-foreground truncate">{v.created_at}</span>
                <Button
                  size="sm"
                  variant="outline"
                  className="ml-auto"
                  disabled={busy === v.id}
                  onClick={() => {
                    setBusy(v.id);
                    restore.mutate(
                      { nodeId: node.id, versionId: v.id },
                      {
                        onSuccess: () => {
                          toast.success(`Restored v${v.version_no}`);
                          close();
                        },
                        onError: (e) => toast.error(e.message),
                        onSettled: () => setBusy(null),
                      },
                    );
                  }}
                >
                  Restore
                </Button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}
