import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { fmtSize, previewKind, type Node } from "@/lib/api";
import { useUiStore } from "@/lib/ui-store";

/**
 * Server-side previews: thumbnail (image/PDF -> PNG) or extracted text.
 * The preview blob is generated asynchronously after upload; a 404 means
 * "not ready yet".
 */
export function PreviewPanel({ node }: { node: Node }) {
  const close = useUiStore((s) => s.closePanel);
  const kind = previewKind(node.mime);
  const url = `/api/v1/nodes/${node.id}/preview?kind=${kind}`;

  if (kind === "thumbnail") {
    return (
      <Dialog open onOpenChange={(v) => !v && close()}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{node.name}</DialogTitle>
            <DialogDescription>
              {node.mime} · {node.size != null ? fmtSize(node.size) : "—"}
            </DialogDescription>
          </DialogHeader>
          <img
            src={url}
            alt={`Preview of ${node.name}`}
            className="mx-auto max-h-[60svh] max-w-full object-contain"
          />
        </DialogContent>
      </Dialog>
    );
  }
  return (
    <Dialog open onOpenChange={(v) => !v && close()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Preview · {node.name}</DialogTitle>
          <DialogDescription>{node.mime}</DialogDescription>
        </DialogHeader>
        <TextPreviewContent url={url} />
      </DialogContent>
    </Dialog>
  );
}

export function PreviewContent({ node }: { node: Node }) {
  const kind = previewKind(node.mime);
  const url = `/api/v1/nodes/${node.id}/preview?kind=${kind}`;

  if (kind === "thumbnail") {
    return (
      <img
        src={url}
        alt={`Preview of ${node.name}`}
        className="max-h-48 w-full rounded-md object-contain"
      />
    );
  }
  return <TextPreviewContent url={url} />;
}

function TextPreviewContent({ url }: { url: string }) {
  const [text, setText] = useState<string | null>(null);
  const [missing, setMissing] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void fetch(url)
      .then(async (res) => {
        if (cancelled) return;
        if (res.ok) {
          setText(await res.text());
        } else {
          setMissing(true);
        }
      })
      .catch(() => !cancelled && setMissing(true));
    return () => {
      cancelled = true;
    };
  }, [url]);

  if (missing) {
    return (
      <p className="text-muted-foreground text-xs">
        Preview not ready yet — the server generates it shortly after upload.
      </p>
    );
  }
  if (text === null) {
    return <p className="text-muted-foreground text-xs">Loading…</p>;
  }
  return (
    <pre className="bg-muted max-h-64 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">
      {text}
    </pre>
  );
}
