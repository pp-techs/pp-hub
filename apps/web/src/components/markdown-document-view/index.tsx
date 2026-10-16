import { lazy, Suspense, useEffect, useRef, useState } from "react";
import type { ReactNode, RefObject } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import type { UseQueryResult } from "@tanstack/react-query";
import { FileWarning } from "lucide-react";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import type { MarkdownDocument, Node } from "@/lib/api";
import { openFile } from "@/lib/open-file";
import { nodeDocumentFiles, shareDocumentFiles } from "@/lib/collab/document-files";
import { usePageStore } from "@/lib/collab/page-store";
import { useNodeDocument, useShareDocument } from "@/lib/queries";
import type { ReadOnlyEditorProps } from "./read-only-editor";
import { MarkdownDocumentSkeleton } from "./skeleton";

export { MarkdownDocumentSkeleton } from "./skeleton";

// The editor is the slash-editor/Tiptap bundle. The drive listing reaches
// this module through the details panel and the preview, so the bundle is
// split off and fetched the first time a document is shown — in parallel
// with the document itself, which the wrappers below already requested.
const ReadOnlyEditor = lazy(() => import("./read-only-editor"));

/**
 * A markdown document rendered read-only: the block editor's own schema and
 * node views on a non-editable Tiptap editor, fed the JSON the server parsed.
 * No collab room is joined — this is a snapshot, so it can show a past
 * version, or a file behind a share link with no session at all.
 *
 * `files` says where this viewer may fetch what the document references —
 * its attachments and the files beside it named by relative paths (the
 * document's private routes, or the share link's) — and how a relative link
 * opens. The editor's extensions are resolved once, so a caller showing a
 * different document remounts the view with a new `key` rather than swapping
 * them; a new `doc` for the same document replaces the content in place.
 *
 * `lossy` says the file holds markdown the block schema has no node for (raw
 * HTML, footnotes…). The render leaves that out, and the reader is told so
 * rather than handed a page that looks complete.
 */
export function MarkdownDocumentView(props: ReadOnlyEditorProps) {
  return (
    <Suspense fallback={<MarkdownDocumentSkeleton className={props.className} />}>
      <ReadOnlyEditor {...props} />
    </Suspense>
  );
}

/**
 * A signed-in reader's view of a markdown file: the current version, or
 * `versionId`. The server gates `/document` on the view capability, as it does
 * the file's bytes. A sub-page, page link or relative link to a page opens in
 * the editor, in a new tab, as a document opened from the drive does.
 */
export function NodeMarkdownView({
  node,
  versionId,
  className,
  outlineScrollRef,
}: {
  node: Node;
  versionId?: string;
  className?: string;
  outlineScrollRef?: RefObject<HTMLElement | null>;
}) {
  const document = useNodeDocument(node.id, versionId);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- source discriminant
  const pageStore = usePageStore({ kind: "node", spaceId: node.space_id });
  if (!document.data) return <DocumentPending query={document} className={className} />;
  return (
    <MarkdownDocumentView
      key={`${node.id}:${versionId ?? ""}`}
      doc={document.data.doc}
      lossy={document.data.lossy}
      files={nodeDocumentFiles(node.id, {
        versionId,
        openPage: (id) => openFile({ id, space_id: node.space_id }),
      })}
      className={className}
      outlineScrollRef={outlineScrollRef}
      pages={{
        store: pageStore,
        pageId: node.id,
        onNavigate: (id) => openFile({ id, space_id: node.space_id }),
      }}
    />
  );
}

/**
 * A markdown file inside a share link, rendered for an anonymous visitor. A
 * sub-page, page link or relative link to a page inside the link's reach
 * calls `onNavigatePage`.
 */
export function SharedMarkdownView({
  token,
  nodeId,
  className,
  outlineScrollRef,
  onNavigatePage,
}: {
  token: string;
  nodeId: string;
  className?: string;
  outlineScrollRef?: RefObject<HTMLElement | null>;
  onNavigatePage: (pageId: string) => void;
}) {
  const document = useShareDocument(token, nodeId);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- source discriminant
  const pageStore = usePageStore({ kind: "share", token });
  if (!document.data) return <DocumentPending query={document} className={className} />;
  return (
    <MarkdownDocumentView
      key={nodeId}
      doc={document.data.doc}
      lossy={document.data.lossy}
      files={shareDocumentFiles(token, nodeId, onNavigatePage)}
      className={className}
      outlineScrollRef={outlineScrollRef}
      pages={{ store: pageStore, pageId: nodeId, onNavigate: onNavigatePage }}
    />
  );
}

/**
 * A markdown file's first screen at half scale, for a card or the details
 * panel: a page's opening lines at a glance, which says more than an icon.
 *
 * It is the real renderer, not an artifact — a thumbnail PNG would need a font
 * covering every script the drive holds (the Go ones lack Vietnamese), and
 * the browser already has them. The price is a document fetch and an editor
 * per card, so neither starts until the card scrolls near the viewport.
 * `fallback` stands in when the document cannot be had.
 *
 * Fills its parent; the parent draws the frame. `inert` keeps links and
 * toggles out of the tab order of a card whose actions are elsewhere.
 */
export function MarkdownThumbnail({ node, fallback }: { node: Node; fallback: ReactNode }) {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- source discriminant
  const pageStore = usePageStore({ kind: "node", spaceId: node.space_id });
  const frame = useRef<HTMLDivElement>(null);
  const [near, setNear] = useState(false);
  useEffect(() => {
    const el = frame.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setNear(true);
          observer.disconnect();
        }
      },
      // eslint-disable-next-line lingui/no-unlocalized-strings -- CSS length
      { rootMargin: "200px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const document = useNodeDocument(near ? node.id : null);
  if (document.isError) return <>{fallback}</>;

  return (
    <div ref={frame} className="relative size-full overflow-hidden">
      <div inert className="pointer-events-none w-[200%] origin-top-left scale-50 select-none">
        {document.data ? (
          <MarkdownDocumentView
            key={node.id}
            doc={document.data.doc}
            lossy={document.data.lossy}
            files={nodeDocumentFiles(node.id, { openPage: () => {} })}
            className="p-6"
            // Inert: nothing opens from a thumbnail, but sub-pages read by name.
            pages={{ store: pageStore, pageId: node.id, onNavigate: () => {} }}
          />
        ) : (
          <MarkdownDocumentSkeleton className="p-6" />
        )}
      </div>
      <div className="from-background pointer-events-none absolute inset-x-0 bottom-0 h-8 bg-gradient-to-t" />
    </div>
  );
}

/** What stands in for a document that has not arrived: its skeleton, or why it never will. */
function DocumentPending({
  query,
  className,
}: {
  query: UseQueryResult<MarkdownDocument>;
  className?: string;
}) {
  const { t } = useLingui();
  if (!query.isError) return <MarkdownDocumentSkeleton className={className} />;
  return (
    <Empty className="py-12">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FileWarning />
        </EmptyMedia>
        <EmptyTitle>
          <Trans>This document cannot be shown</Trans>
        </EmptyTitle>
        <EmptyDescription>
          {query.error instanceof Error && query.error.message
            ? query.error.message
            : t`The server did not answer.`}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
