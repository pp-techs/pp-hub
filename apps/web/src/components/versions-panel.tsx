import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import type { Node } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";
import { VersionList } from "@/components/version-list";

/** Version history as its own dialog, reached from a node's ⋯ menu. */
export function VersionsPanel({ node }: { node: Node }) {
  const close = useUiStore((s) => s.closePanel);

  return (
    <Dialog open onOpenChange={(v) => !v && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Versions of “{node.name}”</DialogTitle>
          <DialogDescription>Every saved revision of this file.</DialogDescription>
        </DialogHeader>
        <VersionList node={node} onRestored={close} />
      </DialogContent>
    </Dialog>
  );
}
