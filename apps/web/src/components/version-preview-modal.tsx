import { createModal } from "@buiducnhat/better-modal";
import { Trans } from "@lingui/react/macro";
import { RotateCcw } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import type { Node, Version } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import { NodeMarkdownView } from "@/components/markdown-document-view";

type Props = {
  node: Node;
  version: Version;
  /** Offer Restore: the caller holds edit and this is not already the current version. */
  canRestore: boolean;
};

/**
 * One version of a markdown document, rendered the way the editor shows it,
 * so a writer can see what a restore would bring back before doing it.
 * Resolves `true` when Restore was chosen; the versions list performs it, so
 * a restore from here and one from the list are the same act.
 */
export const VersionPreviewModal = createModal<Props, boolean>(
  "version-preview",
  ({ node, version, canRestore, modal }) => {
    const f = useFormatters();
    const versionNo = version.version_no;
    const nodeName = node.name;
    const created = f.relative(version.created_at);
    const size = f.size(version.size);

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(false)}>
        <DialogContent className="flex h-[85vh] max-h-[85vh] flex-col gap-0 p-0 !max-w-4xl sm:!max-w-4xl">
          <DialogHeader className="border-border shrink-0 border-b px-4 py-3 pr-12">
            <DialogTitle className="truncate text-sm font-medium">
              <Trans>
                {nodeName} — version {versionNo}
              </Trans>
            </DialogTitle>
            <DialogDescription className="text-[10px] tabular-nums">
              <Trans>
                Saved {created} · {size}
              </Trans>
            </DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto">
            <NodeMarkdownView node={node} versionId={version.id} className="px-8 py-6" />
          </div>
          <DialogFooter className="border-border shrink-0 border-t px-4 py-3">
            <Button variant="outline" onClick={() => modal.resolve(false)}>
              <Trans>Close</Trans>
            </Button>
            {canRestore ? (
              <Button onClick={() => modal.resolve(true)}>
                <RotateCcw data-icon="inline-start" />
                <Trans>Restore this version</Trans>
              </Button>
            ) : null}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  },
);
