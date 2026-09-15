import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog";
import { Button } from "@workspace/ui/components/button";
import { Download, ExternalLink } from "lucide-react";
import { createModal } from "@buiducnhat/better-modal";
import { canOpenInTab, fmtSize, openInTab, type Node } from "@/lib/api";
import { PreviewContent } from "./preview-content";

type Props = { node: Node };
type Result = void;

export const PreviewModal = createModal<Props, Result>("preview", ({ node, modal }) => {
  const downloadUrl = `/api/v1/nodes/${node.id}/download`;

  return (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(undefined)}>
      <DialogContent className="!max-w-[90vw] flex h-[85vh] max-h-[85vh] flex-col p-0 sm:!max-w-[90vw]">
        <DialogHeader className="flex shrink-0 flex-row items-center gap-2 border-b px-4 py-2">
          <DialogTitle className="flex-1 truncate text-sm font-medium" title={node.name}>
            {node.name}
          </DialogTitle>
          {canOpenInTab(node) && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1 text-xs"
              onClick={() => openInTab(node)}
            >
              <ExternalLink className="size-3.5" />
              Open
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 text-xs"
            onClick={() => window.open(downloadUrl, "_blank")}
          >
            <Download className="size-3.5" />
            Download
          </Button>
          <span className="text-muted-foreground ml-2 text-xs">
            {node.mime ?? "file"} · {node.size != null ? fmtSize(node.size) : "—"}
          </span>
        </DialogHeader>

        {/* Preview content — all supported kinds */}
        <div className="min-h-0 flex flex-1 flex-col overflow-auto">
          <PreviewContent node={node} />
        </div>
      </DialogContent>
    </Dialog>
  );
});
