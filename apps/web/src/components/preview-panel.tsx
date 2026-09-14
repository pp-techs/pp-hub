import { X, Download, ExternalLink } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { fmtSize, isOfficeFile, openEditor, type Node } from "@/lib/api";
import { PreviewContent } from "./preview-content";

/**
 * Slide-in details panel (right sidebar). Shows when a node is selected.
 * Works for files AND folders.
 */
export function DetailsPanel({ node, onClose }: { node: Node; onClose: () => void }) {
  const downloadUrl = `/api/v1/nodes/${node.id}/download`;

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Header */}
      <div className="flex shrink-0 flex-row items-center gap-2 border-b px-3 py-2">
        <span className="flex-1 truncate text-sm font-medium" title={node.name}>
          {node.name}
        </span>
        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose}>
          <X className="size-4" />
        </Button>
      </div>

      {/* Preview area (files only) */}
      {node.kind === "file" && <PreviewContent node={node} />}

      {/* Actions footer (files only) */}
      {node.kind === "file" && (
        <div className="flex shrink-0 flex-row items-center gap-2 border-t px-3 py-2">
          {/* Open with (office files) */}
          {isOfficeFile(node.name) && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-1 text-xs"
              onClick={() => openEditor(node)}
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
          <span className="text-muted-foreground ml-auto text-xs">
            {node.mime ?? "file"} · {node.size != null ? fmtSize(node.size) : "—"}
          </span>
        </div>
      )}
    </div>
  );
}
