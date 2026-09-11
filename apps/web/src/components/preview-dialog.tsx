import { useEffect, useState } from "react";
import { Dialog, DialogContent } from "@workspace/ui/components/dialog";
import { Button } from "@workspace/ui/components/button";
import { Download, ExternalLink } from "lucide-react";
import { fmtSize, previewKind, isOfficeFile, openEditor, type Node } from "@/lib/api";

/**
 * Full-screen preview dialog, opened on double-click.
 * Uses shadcn Dialog component.
 */
export function PreviewDialog({ node, onClose }: { node: Node | null; onClose: () => void }) {
  const kind = previewKind(node?.mime);
  const previewUrl = node ? `/api/v1/nodes/${node.id}/preview?kind=${kind}` : null;
  const downloadUrl = node ? `/api/v1/nodes/${node.id}/download` : null;

  return (
    <Dialog open={!!node} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-4xl flex h-[85vh] max-h-[85vh] flex-col p-0">
        {/* Toolbar */}
        <div className="flex shrink-0 items-center gap-2 border-b px-4 py-2">
          <span className="flex-1 truncate text-sm font-medium" title={node?.name}>
            {node?.name}
          </span>

          {node && (
            <>
              {/* Open with (office files) */}
              {isOfficeFile(node.name) && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 gap-1 text-xs"
                  onClick={() => {
                    onClose();
                    openEditor(node);
                  }}
                >
                  <ExternalLink className="size-3.5" />
                  Open
                </Button>
              )}

              {/* Download */}
              <Button
                variant="ghost"
                size="sm"
                className="h-7 gap-1 text-xs"
                onClick={() => window.open(downloadUrl!, "_blank")}
              >
                <Download className="size-3.5" />
                Download
              </Button>

              {/* Footer: size info */}
              <span className="text-muted-foreground ml-2 text-xs">
                {node.mime ?? "file"} · {node.size != null ? fmtSize(node.size) : "—"}
              </span>
            </>
          )}
        </div>

        {/* Preview content */}
        <div className="min-h-0 flex-1 overflow-auto">
          {node && previewUrl && (
            <>
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
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
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
      <p className="text-muted-foreground p-8 text-center text-sm">
        Preview not available for this file.
      </p>
    );
  }
  if (text === null) {
    return (
      <div className="flex items-center justify-center p-8">
        <p className="text-muted-foreground text-sm">Loading…</p>
      </div>
    );
  }
  return (
    <pre className="bg-muted m-4 overflow-auto rounded-md p-4 text-sm whitespace-pre-wrap">
      {text}
    </pre>
  );
}
