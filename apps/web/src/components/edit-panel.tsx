import { useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { childrenOptions, useEditSession } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";
import type { Node } from "@/lib/api";

/**
 * OnlyOffice editor in a dialog. The server's edit-session issues a WOPI
 * access token + editor URL; on close we refetch the folder listing so the
 * saved version shows up.
 */
export function EditPanel({ node }: { node: Node }) {
  const close = useUiStore((s) => s.closePanel);
  const edit = useEditSession();
  const children = childrenOptions(node.space_id, node.parent_id);

  useEffect(() => {
    edit.mutate(node.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node.id]);

  const { data: session, isPending, error } = edit;

  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v) {
          void children.refetch();
          close();
        }
      }}
    >
      <DialogContent className="sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>Editing · {node.name}</DialogTitle>
          <DialogDescription>
            Autosave is handled by OnlyOffice; close this dialog when you are done.
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <p className="text-destructive text-sm">{(error as Error).message}</p>
        ) : isPending ? (
          <p className="text-muted-foreground flex h-[40svh] items-center justify-center text-sm">
            Opening editor…
          </p>
        ) : (
          <iframe
            src={session!.editor_url}
            title={`Editing ${node.name}`}
            className="h-[70svh] w-full rounded-md border"
            allow="clipboard-write"
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
