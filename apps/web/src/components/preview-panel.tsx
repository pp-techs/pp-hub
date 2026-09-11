import { useEffect, useState } from "react";
import { X, Download, ExternalLink } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { fmtSize, previewKind, isOfficeFile, type Node } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";

/**
 * Slide-in details panel (right sidebar). Shows when a node is selected.
 * Works for files AND folders.
 */
export function DetailsPanel({ node, onClose }: { node: Node; onClose: () => void }) {
  const openPanel = useUiStore((s) => s.openPanel);
  const kind = previewKind(node.mime);
  const previewUrl = `/api/v1/nodes/${node.id}/preview?kind=${kind}`;
  const downloadUrl = `/api/v1/nodes/${node.id}/download`;

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Toolbar */}
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <span className="flex-1 truncate text-sm font-medium" title={node.name}>
          {node.name}
        </span>

        {/* Open with (office files) */}
        {node.kind === "file" && isOfficeFile(node.name) && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 text-xs"
            onClick={() => openPanel("edit", node)}
          >
            <ExternalLink className="size-3.5" />
            Open
          </Button>
        )}

        {/* Download */}
        {node.kind === "file" && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 text-xs"
            onClick={() => window.open(downloadUrl, "_blank")}
          >
            <Download className="size-3.5" />
            Download
          </Button>
        )}

        {/* Close */}
        <Button variant="ghost" size="icon-xs" className="shrink-0" onClick={onClose}>
          <X />
        </Button>
      </div>

      {/* Preview content (files only) */}
      {node.kind === "file" ? (
        <div className="min-h-0 flex-1 overflow-auto">
          {kind === "thumbnail" && (
            <img
              src={previewUrl}
              alt={`Preview of ${node.name}`}
              className="mx-auto max-h-full max-w-full object-contain"
            />
          )}
          {kind === "pdf" && <PdfPreview url={previewUrl} />}
          {kind === "video" && <VideoPreview url={previewUrl} />}
          {kind === "text" && <TextPreview url={previewUrl} />}
        </div>
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center">
          <p className="text-muted-foreground text-4xl">📁</p>
          <p className="text-muted-foreground text-sm">Folder</p>
        </div>
      )}

      {/* Footer: size info */}
      <div className="shrink-0 border-t px-3 py-1.5">
        <p className="text-muted-foreground text-xs">
          {node.kind === "file" ? (node.mime ?? "file") : "Folder"} ·{" "}
          {node.size != null ? fmtSize(node.size) : "—"}
        </p>
      </div>
    </div>
  );
}

// ── PDF preview ───────────────────────────────────────────────────────────────

function PdfPreview({ url }: { url: string }) {
  return <iframe src={url} className="h-full w-full border-0" title="PDF preview" />;
}

// ── Video preview ─────────────────────────────────────────────────────────────

function VideoPreview({ url }: { url: string }) {
  return (
    <video controls className="mx-auto max-h-full max-w-full" preload="metadata">
      <source src={url} />
    </video>
  );
}

// ── Text preview ──────────────────────────────────────────────────────────────

function TextPreview({ url }: { url: string }) {
  const [text, setText] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);

  useEffect(() => {
    setText(null);
    setMissing(false);
    fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error("not found");
        return r.text();
      })
      .then(setText)
      .catch(() => setMissing(true));
  }, [url]);

  if (missing) {
    return (
      <p className="text-muted-foreground p-4 text-xs">Preview not available for this file.</p>
    );
  }
  if (text === null) {
    return <p className="text-muted-foreground p-4 text-xs">Loading…</p>;
  }
  return (
    <pre className="bg-muted m-4 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">
      {text}
    </pre>
  );
}

// ── Inline preview (used in right sidebar when a file is selected) ─────────────

export function PreviewContent({ node }: { node: Node }) {
  const kind = previewKind(node.mime);
  const previewUrl = `/api/v1/nodes/${node.id}/preview?kind=${kind}`;

  return (
    <div className="min-h-0 flex-1 overflow-auto">
      {kind === "thumbnail" && (
        <img
          src={previewUrl}
          alt={`Preview of ${node.name}`}
          className="mx-auto max-h-full max-w-full object-contain"
        />
      )}
      {kind === "pdf" && <PdfPreview url={previewUrl} />}
      {kind === "video" && <VideoPreview url={previewUrl} />}
      {kind === "text" && <TextPreview url={previewUrl} />}
    </div>
  );
}
