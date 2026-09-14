import { useEffect, useState } from "react";
import { ExternalLink } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import type { Node } from "@/lib/api";
import { isOfficeFile, openEditor, previewKind } from "@/lib/api";

// ── Fetching ──────────────────────────────────────────────────────────────────

type PreviewState = { status: "loading" } | { status: "missing" } | { status: "ready"; blob: Blob };

/**
 * Fetches a preview blob. The endpoint 404s with problem+json ("preview not
 * ready") whenever the worker hasn't produced a blob for this version yet, so
 * we resolve it here rather than letting an <iframe> render that JSON body.
 */
function usePreviewBlob(url: string): PreviewState {
  const [state, setState] = useState<PreviewState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });

    fetch(url)
      .then(async (r) => {
        if (!r.ok) throw new Error(`preview ${r.status}`);
        const blob = await r.blob();
        if (!cancelled) setState({ status: "ready", blob });
      })
      .catch(() => {
        if (!cancelled) setState({ status: "missing" });
      });

    return () => {
      cancelled = true;
    };
  }, [url]);

  return state;
}

// ── Messages ──────────────────────────────────────────────────────────────────

function Message({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2 p-4 text-center text-xs">
      {children}
    </div>
  );
}

const NOT_READY = "Preview not available yet — it is generated shortly after upload.";

// ── Thumbnail preview ─────────────────────────────────────────────────────────

function ThumbnailPreview({ url }: { url: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return <Message>{NOT_READY}</Message>;
  return (
    <img
      src={url}
      alt=""
      className="mx-auto max-h-full max-w-full object-contain"
      onError={() => setFailed(true)}
    />
  );
}

// ── PDF preview ───────────────────────────────────────────────────────────────

function PdfPreview({ url }: { url: string }) {
  const state = usePreviewBlob(url);
  const [src, setSrc] = useState<string | null>(null);

  useEffect(() => {
    if (state.status !== "ready") return;
    const objectUrl = URL.createObjectURL(state.blob);
    setSrc(objectUrl);
    return () => URL.revokeObjectURL(objectUrl);
  }, [state]);

  if (state.status === "missing") return <Message>{NOT_READY}</Message>;
  if (!src) return <Message>Loading…</Message>;
  return <iframe src={src} className="h-full w-full border-0" title="PDF preview" />;
}

// ── Video preview ─────────────────────────────────────────────────────────────

function VideoPreview({ url }: { url: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return <Message>{NOT_READY}</Message>;
  return (
    <video
      controls
      className="mx-auto max-h-full max-w-full"
      preload="metadata"
      src={url}
      onError={() => setFailed(true)}
    />
  );
}

// ── Text preview ──────────────────────────────────────────────────────────────

function TextPreview({ url }: { url: string }) {
  const state = usePreviewBlob(url);
  const [text, setText] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (state.status !== "ready") return;
    let cancelled = false;
    setFailed(false);
    state.blob
      .text()
      .then((t) => {
        if (!cancelled) setText(t);
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [state]);

  if (state.status === "missing" || failed) return <Message>{NOT_READY}</Message>;
  if (text === null) return <Message>Loading…</Message>;
  return (
    <pre className="bg-muted m-4 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">
      {text}
    </pre>
  );
}

// ── PreviewContent (shared) ───────────────────────────────────────────────────

export function PreviewContent({ node }: { node: Node }) {
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      <PreviewBody node={node} />
    </div>
  );
}

function PreviewBody({ node }: { node: Node }) {
  const kind = previewKind(node.mime);

  if (kind === null) {
    if (!isOfficeFile(node.name)) {
      return <Message>No preview available — use Download to view this file.</Message>;
    }
    return (
      <Message>
        <span>This file opens in the office editor.</span>
        <Button variant="outline" size="sm" className="gap-1" onClick={() => openEditor(node)}>
          <ExternalLink className="size-3.5" />
          Open
        </Button>
      </Message>
    );
  }

  const url = `/api/v1/nodes/${node.id}/preview?kind=${kind}`;
  switch (kind) {
    case "thumbnail":
      return <ThumbnailPreview url={url} />;
    case "pdf":
      return <PdfPreview url={url} />;
    case "video":
      return <VideoPreview url={url} />;
    case "text":
      return <TextPreview url={url} />;
  }
}
