import { useEffect, useState } from "react";
import { ExternalLink } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import type { Node } from "@/lib/api";
import { contentUrl, isOfficeFile, openEditor, openInTab, previewKind } from "@/lib/api";

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

/**
 * PDFs render from `/content` — the file's own bytes — rather than from
 * `/preview`. The worker's `pdf` blob is a placeholder marker, not a rendered
 * page (docs/architecture/preview-pipeline.md), so the browser's built-in
 * viewer is both the fastest and the only real option today.
 *
 * <object> rather than <iframe>: browsers with no PDF plugin render the
 * children instead of a blank frame.
 */
function PdfPreview({ node }: { node: Node }) {
  return (
    <object
      data={contentUrl(node)}
      type="application/pdf"
      className="h-full min-h-[60vh] w-full"
      aria-label={`PDF preview of ${node.name}`}
    >
      <Message>
        <span>This browser can&apos;t display PDFs inline.</span>
        <Button variant="outline" size="sm" className="gap-1" onClick={() => openInTab(node)}>
          <ExternalLink className="size-3.5" />
          Open in new tab
        </Button>
      </Message>
    </object>
  );
}

// ── Video preview ─────────────────────────────────────────────────────────────

/** Plays the stored file itself; `/content` answers Range requests so it seeks. */
function VideoPreview({ node }: { node: Node }) {
  const [failed, setFailed] = useState(false);
  if (failed) return <Message>This video can&apos;t be played here — use Download.</Message>;
  return (
    <video
      controls
      className="mx-auto max-h-full max-w-full"
      preload="metadata"
      src={contentUrl(node)}
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

  // thumbnail/text come from the generated preview blob; pdf/video stream the
  // original file, whose generated "preview" is only a placeholder marker.
  const url = `/api/v1/nodes/${node.id}/preview?kind=${kind}`;
  switch (kind) {
    case "thumbnail":
      return <ThumbnailPreview url={url} />;
    case "pdf":
      return <PdfPreview node={node} />;
    case "video":
      return <VideoPreview node={node} />;
    case "text":
      return <TextPreview url={url} />;
  }
}
