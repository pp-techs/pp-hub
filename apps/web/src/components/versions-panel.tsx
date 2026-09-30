import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Trans } from "@lingui/react/macro";
import type { Node } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";
import { VersionList } from "@/components/version-list";

/** Version history as its own dialog, reached from a node's ⋯ menu. */
export function VersionsPanel({ node }: { node: Node }) {
  const close = useUiStore((s) => s.closePanel);
  const nodeName = node.name;

  return (
    <Dialog open onOpenChange={(v) => !v && close()}>
      {/* Capped to the viewport: the header stays put and only the history
          scrolls, however many versions the file has. */}
      <DialogContent className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_minmax(0,1fr)] sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            <Trans>Versions of “{nodeName}”</Trans>
          </DialogTitle>
          <DialogDescription>
            <Trans>Every saved revision of this file.</Trans>
          </DialogDescription>
        </DialogHeader>
        {/* pl-1: the timeline dots overhang the list's rule by 2.5px, and a
            scroller clips on both axes. */}
        <div className="min-h-0 overflow-y-auto pl-1">
          <VersionList node={node} onRestored={close} />
        </div>
      </DialogContent>
    </Dialog>
  );
}
