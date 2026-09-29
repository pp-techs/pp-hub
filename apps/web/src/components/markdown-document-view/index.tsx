import { lazy, Suspense } from "react";
import type { RefObject } from "react";
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
import { attachmentUrl, shareAttachmentUrl, type MarkdownDocument, type Node } from "@/lib/api";
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
 * `attachmentUrl` maps an `attachment:<hash>` reference to where this viewer
 * may fetch it (the document's private route, or the share link's). The
 * editor's extensions are resolved once, so a caller showing a different
 * document remounts the view with a new `key` rather than swapping the
 * resolver; a new `doc` for the same document replaces the content in place.
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
 * the file's bytes.
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
  if (!document.data) return <DocumentPending query={document} className={className} />;
  return (
    <MarkdownDocumentView
      key={`${node.id}:${versionId ?? ""}`}
      doc={document.data.doc}
      lossy={document.data.lossy}
      attachmentUrl={(hash) => attachmentUrl(node.id, hash)}
      className={className}
      outlineScrollRef={outlineScrollRef}
    />
  );
}

/** A markdown file inside a share link, rendered for an anonymous visitor. */
export function SharedMarkdownView({
  token,
  nodeId,
  className,
  outlineScrollRef,
}: {
  token: string;
  nodeId: string;
  className?: string;
  outlineScrollRef?: RefObject<HTMLElement | null>;
}) {
  const document = useShareDocument(token, nodeId);
  if (!document.data) return <DocumentPending query={document} className={className} />;
  return (
    <MarkdownDocumentView
      key={nodeId}
      doc={document.data.doc}
      lossy={document.data.lossy}
      attachmentUrl={(hash) => shareAttachmentUrl(token, nodeId, hash)}
      className={className}
      outlineScrollRef={outlineScrollRef}
    />
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
