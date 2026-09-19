import { useEffect, useState } from "react";
import { ExternalLink } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import type { Node } from "@/lib/api";
import {
  capsOf,
  contentUrl,
  isOfficeFile,
  openEditor,
  openInTab,
  previewKind,
  previewUrl,
} from "@/lib/api";

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

// ── Image preview ─────────────────────────────────────────────────────────────

/** Centres a media element in the viewer and lets it use the whole area. */
function Stage({ children }: { children: React.ReactNode }) {
  return <div className="flex h-full min-h-full items-center justify-center p-4">{children}</div>;
}

/**
 * Images render from `/content` — the file's own bytes — not from the
 * generated `thumbnail` blob, which is capped at 256px on its longest side
 * (docs/architecture/preview-pipeline.md) and shows as a stamp in a full-size
 * viewer. The thumbnail stays as the fallback for an image whose bytes the
 * server won't serve inline — including to a reader with no download
 * capability, for whom the stamp is the whole of what they may see.
 */
function ImagePreview({
  node,
  thumbUrl,
  canRead,
}: {
  node: Node;
  thumbUrl: string;
  canRead: boolean;
}) {
  const [src, setSrc] = useState(canRead ? contentUrl(node) : thumbUrl);
  const [failed, setFailed] = useState(false);

  if (failed) return <Message>{NOT_READY}</Message>;
  return (
    <Stage>
      <img
        src={src}
        alt={node.name}
        className="max-h-full max-w-full object-contain"
        onError={() => {
          if (src === thumbUrl) setFailed(true);
          else setSrc(thumbUrl);
        }}
      />
    </Stage>
  );
}

// ── PDF preview ───────────────────────────────────────────────────────────────

/**
 * PDFs render from `/content` — the file's own bytes — rather than from
 * `/preview`. The generated blob is a 256px thumbnail, right for a grid card
 * and useless as a document viewer, so the browser's built-in viewer reads the
 * original (docs/architecture/preview-pipeline.md).
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
    <Stage>
      <video
        controls
        className="max-h-full max-w-full"
        preload="metadata"
        src={contentUrl(node)}
        onError={() => setFailed(true)}
      />
    </Stage>
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
  const mime = node.mime?.split(";")[0]?.trim() ?? "";
  // `/content` is the file itself and takes the download capability, so a
  // reader who was refused it is shown what the preview worker derived
  // instead: a thumbnail, or the text snippet. There is no full-size render
  // to fall back to, and pretending otherwise would just 403 in an <img>.
  const canRead = capsOf(node).download;

  // Dispatch on MIME, not on preview kind. The preview kind says what the
  // worker *stored* — and it stores one thumbnail for images, PDFs and video
  // alike — which is not enough to choose between <img>, <object> and <video>.
  // What the browser can render inline is a property of the MIME type.
  if (canRead) {
    if (mime === "application/pdf") return <PdfPreview node={node} />;
    if (mime.startsWith("video/")) return <VideoPreview node={node} />;
  }
  if (mime.startsWith("image/")) {
    return <ImagePreview node={node} thumbUrl={previewUrl(node)} canRead={canRead} />;
  }
  if (previewKind(node.mime) === "text") {
    return <TextPreview url={previewUrl(node, "text")} />;
  }
  if (!canRead) {
    return (
      <Message>Downloads are off for this file, so it can only be opened by its owner.</Message>
    );
  }

  if (isOfficeFile(node.name)) {
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
  return <Message>No preview available — use Download to view this file.</Message>;
}
