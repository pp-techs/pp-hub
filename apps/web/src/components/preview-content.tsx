import { useEffect, useMemo, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import { ExternalLink } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { LoadingStatus } from "@/components/loading";
import { NodeMarkdownView } from "@/components/markdown-document-view";
import { mdocLowlight } from "@/lib/collab/mdoc-kit";
import { useFormatters } from "@/lib/format";
import type { Node } from "@/lib/api";
import {
  capsOf,
  contentUrl,
  isMarkdownFile,
  isOfficeFile,
  isTextFile,
  openEditor,
  openInTab,
  previewKind,
  previewUrl,
  textLanguage,
} from "@/lib/api";

// ── Fetching ──────────────────────────────────────────────────────────────────

type PreviewState = { status: "loading" } | { status: "missing" } | { status: "ready"; blob: Blob };

/**
 * Fetches a preview blob. The endpoint 404s with problem+json ("preview not
 * ready") whenever the worker hasn't produced a blob for this version yet, so
 * we resolve it here rather than letting an <iframe> render that JSON body.
 */
function usePreviewBlob(url: string): PreviewState {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
  const [state, setState] = useState<PreviewState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
    setState({ status: "loading" });

    fetch(url)
      .then(async (r) => {
        // eslint-disable-next-line lingui/no-unlocalized-strings -- developer-facing Error text
        if (!r.ok) throw new Error(`preview ${r.status}`);
        const blob = await r.blob();
        // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
        if (!cancelled) setState({ status: "ready", blob });
      })
      .catch(() => {
        // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
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

/**
 * Said by two viewers — an image whose bytes will not load, and a text
 * artifact the worker has not written yet — so it is one message rather than
 * two. A descriptor, not a string: this is module scope, evaluated at import
 * time before a catalog exists, and rendered text here would freeze whichever
 * language loaded first. Each caller resolves it with `i18n._` at render.
 */
const NOT_READY = msg`Preview not available yet — it is generated shortly after upload.`;

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
  const { i18n } = useLingui();
  const [src, setSrc] = useState(canRead ? contentUrl(node) : thumbUrl);
  const [failed, setFailed] = useState(false);

  if (failed) return <Message>{i18n._(NOT_READY)}</Message>;
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
  const { t } = useLingui();
  const name = node.name;

  return (
    <object
      data={contentUrl(node)}
      type="application/pdf"
      className="h-full min-h-[60vh] w-full"
      aria-label={t`PDF preview of ${name}`}
    >
      <Message>
        <span>
          <Trans>This browser can&apos;t display PDFs inline.</Trans>
        </span>
        <Button variant="outline" size="sm" className="gap-1" onClick={() => openInTab(node)}>
          <ExternalLink className="size-3.5" />
          <Trans>Open in new tab</Trans>
        </Button>
      </Message>
    </object>
  );
}

// ── Video preview ─────────────────────────────────────────────────────────────

/** Plays the stored file itself; `/content` answers Range requests so it seeks. */
function VideoPreview({ node }: { node: Node }) {
  const [failed, setFailed] = useState(false);
  if (failed) {
    return (
      <Message>
        <Trans>This video can&apos;t be played here — use Download.</Trans>
      </Message>
    );
  }
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

// ── Code preview ──────────────────────────────────────────────────────────────

/**
 * How much of a file the viewer reads. Past it the file is cut and says so: a
 * megabyte of log is not something to lay out and colour for a preview, and
 * the rest is one Download away.
 */
const CODE_PREVIEW_MAX_BYTES = 256 * 1024;

type CodeState =
  | { status: "loading" }
  | { status: "failed" }
  | { status: "binary" }
  | { status: "ready"; text: string };

/**
 * Reads at most `limit` bytes of a file's own bytes and decodes them. The
 * `Range` header lets the server stop early; a backend that cannot seek
 * answers 200 with everything, so the stream is also cut client-side.
 */
async function readCode(url: string, limit: number, signal: AbortSignal): Promise<CodeState> {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- HTTP Range header value
  const res = await fetch(url, { headers: { Range: `bytes=0-${limit - 1}` }, signal });
  // eslint-disable-next-line lingui/no-unlocalized-strings -- developer-facing Error text
  if (!res.ok || !res.body) throw new Error(`content ${res.status}`);
  const reader = res.body.getReader();
  const chunks: Uint8Array<ArrayBuffer>[] = [];
  let got = 0;
  while (got < limit) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value as Uint8Array<ArrayBuffer>);
    got += value.length;
  }
  void reader.cancel();
  const bytes = new Uint8Array(await new Blob(chunks).slice(0, limit).arrayBuffer());
  // A NUL in the first page of bytes is how a binary announces itself; text
  // in any encoding the viewer can decode has none.
  // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
  if (bytes.subarray(0, 8192).includes(0)) return { status: "binary" };
  // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
  return { status: "ready", text: new TextDecoder().decode(bytes) };
}

function useCode(url: string): CodeState {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
  const [state, setState] = useState<CodeState>({ status: "loading" });

  useEffect(() => {
    const ctrl = new AbortController();
    // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
    setState({ status: "loading" });
    readCode(url, CODE_PREVIEW_MAX_BYTES, ctrl.signal)
      .then(setState)
      .catch(() => {
        // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
        if (!ctrl.signal.aborted) setState({ status: "failed" });
      });
    return () => ctrl.abort();
  }, [url]);

  return state;
}

/** The part of lowlight's tree (hast) the viewer reads: text, and elements that carry classes. */
interface HighlightNode {
  type: string;
  value?: string;
  properties?: { className?: unknown };
  children?: HighlightNode[];
}

/** Lowlight's tree as React: elements are `<span class="hljs-…">`, the rest is text. */
function HighlightTree({ nodes }: { nodes: HighlightNode[] }) {
  return nodes.map((n, i) => {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- hast node type
    if (n.type === "text") return n.value;
    // eslint-disable-next-line lingui/no-unlocalized-strings -- hast node type
    if (n.type !== "element") return null;
    const cls = n.properties?.className;
    return (
      // eslint-disable-next-line react/no-array-index-key -- a static tree, never reordered
      <span key={i} className={Array.isArray(cls) ? cls.join(" ") : undefined}>
        <HighlightTree nodes={n.children ?? []} />
      </span>
    );
  });
}

/**
 * Text and source code, read from `/content` — the file's own bytes, the same
 * read right as an image or PDF — in full rather than the 4 KiB `?kind=text`
 * snippet. Coloured when highlight.js has the language, with a line-number
 * gutter; lines never wrap, so the gutter stays aligned and the viewer
 * scrolls sideways instead.
 */
function CodePreview({ node }: { node: Node }) {
  const { i18n } = useLingui();
  const f = useFormatters();
  const state = useCode(contentUrl(node));
  const language = textLanguage(node);

  const ready = state.status === "ready" ? state.text : null;
  const lines = useMemo(() => {
    if (ready === null) return null;
    // eslint-disable-next-line lingui/no-unlocalized-strings -- newline
    const count = ready.split("\n").length - (ready.endsWith("\n") ? 1 : 0);
    return Array.from({ length: Math.max(count, 1) }, (_, i) => i + 1).join("\n");
  }, [ready]);
  const code = useMemo(() => {
    if (ready === null || !language || !mdocLowlight.registered(language)) return ready;
    return <HighlightTree nodes={mdocLowlight.highlight(language, ready).children} />;
  }, [ready, language]);

  // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
  if (state.status === "failed") return <Message>{i18n._(NOT_READY)}</Message>;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- union discriminator
  if (state.status === "binary") {
    return (
      <Message>
        <Trans>This file is not text, so it cannot be previewed.</Trans>
      </Message>
    );
  }
  if (lines === null) {
    return (
      <LoadingStatus className="h-full p-4">
        <Skeleton className="h-full min-h-40 w-full" />
      </LoadingStatus>
    );
  }
  const truncated = (node.size ?? 0) > CODE_PREVIEW_MAX_BYTES;
  const shown = f.size(CODE_PREVIEW_MAX_BYTES);
  const total = f.size(node.size ?? 0);
  return (
    <div className="code-preview flex min-h-full flex-col">
      <div className="flex flex-1 font-mono text-xs leading-relaxed">
        <pre
          aria-hidden
          className="bg-background text-muted-foreground border-border sticky left-0 shrink-0 border-r px-3 py-3 text-right select-none"
        >
          {lines}
        </pre>
        <pre className="flex-1 px-3 py-3 whitespace-pre">
          <code>{code}</code>
        </pre>
      </div>
      {truncated && (
        <p className="bg-background text-muted-foreground border-border sticky bottom-0 left-0 border-t px-3 py-1.5 text-xs">
          <Trans>
            Showing the first {shown} of {total} — use Download for the rest.
          </Trans>
        </p>
      )}
    </div>
  );
}

// ── Text preview ──────────────────────────────────────────────────────────────

function TextPreview({ url }: { url: string }) {
  const { i18n } = useLingui();
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

  if (state.status === "missing" || failed) return <Message>{i18n._(NOT_READY)}</Message>;
  if (text === null)
    return (
      <LoadingStatus className="h-full p-4">
        <Skeleton className="h-full min-h-40 w-full" />
      </LoadingStatus>
    );
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
  // `/content` is the file itself and takes view, so anyone who can reach the
  // node can render what the browser renders inline. Download is a separate
  // right: it adds the save button, not the power to read.
  const canRead = capsOf(node).view;

  // Dispatch on MIME, not on preview kind. The preview kind says what the
  // worker *stored* — and it stores one thumbnail for images, PDFs and video
  // alike — which is not enough to choose between <img>, <object> and <video>.
  // What the browser can render inline is a property of the MIME type.
  if (canRead) {
    // Text goes first and by name: the stored MIME says `.ts` is video/mp2t
    // and `.go` is an opaque download, neither of which is what is in the file.
    if (isTextFile(node)) return <CodePreview node={node} />;
    if (mime === "application/pdf") return <PdfPreview node={node} />;
    // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
    if (mime.startsWith("video/")) return <VideoPreview node={node} />;
  }
  // A markdown document reads as what it is, laid out the way the editor
  // shows it, rather than as its source text. Same capability as the bytes.
  if (canRead && isMarkdownFile(node)) {
    return <NodeMarkdownView node={node} className="px-8 py-6" />;
  }
  // eslint-disable-next-line lingui/no-unlocalized-strings -- MIME type prefix
  if (mime.startsWith("image/")) {
    return <ImagePreview node={node} thumbUrl={previewUrl(node)} canRead={canRead} />;
  }
  // The text artifact is the file's words, in miniature; the server gates
  // `?kind=text` on view like the file itself. Asking for it without the
  // capability is a guaranteed 403 rendered as "preview not ready", which is
  // a lie about why.
  if (canRead && previewKind(node.mime) === "text") {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- preview artifact kind
    return <TextPreview url={previewUrl(node, "text")} />;
  }
  if (!canRead) {
    return (
      <Message>
        <Trans>You do not have access to this file.</Trans>
      </Message>
    );
  }

  if (isOfficeFile(node.name)) {
    return (
      <Message>
        <span>
          <Trans>This file opens in the office editor.</Trans>
        </span>
        <Button variant="outline" size="sm" className="gap-1" onClick={() => openEditor(node)}>
          <ExternalLink className="size-3.5" />
          <Trans>Open</Trans>
        </Button>
      </Message>
    );
  }
  return (
    <Message>
      <Trans>No preview available — use Download to view this file.</Trans>
    </Message>
  );
}
