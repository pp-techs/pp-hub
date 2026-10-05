import { findNodeById, type EmbedOptions } from "@slash-editor/core";
import { useResolvedSrc } from "@slash-editor/react";
import type { Editor } from "@tiptap/core";
import { NodeViewWrapper, useEditorState, type NodeViewProps } from "@tiptap/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { GlobeIcon, LinkIcon } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { EmbedLinkModal } from "@/components/nodes/embed-link-modal";

interface EmbedAttrs {
  id: string;
  url: string | null;
  title: string | null;
  description: string | null;
  thumbnail: string | null;
}

/**
 * Asks for a link and writes what the server read from it into the embed
 * block `id`. The block is found again by id once the dialog closes — the
 * document may have moved under it meanwhile, collaborators included. A
 * block inserted just for this (`removeIfCancelled`, the slash item) is
 * removed again when the dialog is dismissed, so no empty card is left over.
 */
export function pickEmbedLink(
  editor: Editor,
  documentId: string,
  id: string,
  { removeIfCancelled }: { removeIfCancelled: boolean },
): void {
  EmbedLinkModal.show({ documentId }).then(
    (link) => {
      const found = findNodeById(editor.state.doc, id);
      if (!found || editor.isDestroyed) return;
      const { tr } = editor.state;
      if (link) {
        tr.setNodeMarkup(found.pos, undefined, {
          ...found.node.attrs,
          url: link.url,
          // eslint-disable-next-line lingui/no-unlocalized-strings -- embed mode attr
          mode: "bookmark",
          title: link.title || null,
          description: link.description || null,
          thumbnail: link.thumbnail ?? null,
        });
      } else if (removeIfCancelled && !found.node.attrs.url) {
        tr.delete(found.pos, found.pos + found.node.nodeSize);
      } else {
        return;
      }
      editor.view.dispatch(tr);
      editor.commands.focus();
    },
    // Reopened or removed before an answer: nothing was decided.
    () => undefined,
  );
}

/**
 * A link as a bookmark card: the page's title, description, host and
 * thumbnail (an attachment of this document — pp-hub never hotlinks another
 * site's image — resolved through the node's `resolveSrc`). An `iframe` embed
 * from imported markdown renders as the same card: pp-hub does not frame
 * other sites. An empty block offers the link dialog to a writer
 * (`documentId` given) and says nothing to a reader.
 */
export function EmbedNodeView({
  editor,
  node,
  selected,
  extension,
  documentId,
}: NodeViewProps & { documentId?: string }) {
  const { t } = useLingui();
  const attrs = node.attrs as EmbedAttrs;
  const editable =
    useEditorState({ editor, selector: ({ editor: e }) => e.isEditable }) && !!documentId;
  const thumbnail = useResolvedSrc(
    attrs.thumbnail,
    node,
    (extension.options as EmbedOptions).resolveSrc,
  );

  if (!attrs.url) {
    return (
      <NodeViewWrapper
        contentEditable={false}
        className="border-border bg-muted/40 text-muted-foreground my-2 flex items-center gap-2 rounded-md border border-dashed p-4"
      >
        <LinkIcon className="size-4 shrink-0" />
        <span className="flex-1 truncate text-sm">{t`Embed a link`}</span>
        {documentId && editable ? (
          <Button
            type="button"
            size="sm"
            variant="secondary"
            onMouseDown={(event) => event.preventDefault()}
            onClick={() =>
              pickEmbedLink(editor, documentId, attrs.id, { removeIfCancelled: false })
            }
          >
            <Trans>Add link</Trans>
          </Button>
        ) : null}
      </NodeViewWrapper>
    );
  }

  let host = attrs.url;
  try {
    host = new URL(attrs.url).host;
  } catch {
    // Not a URL the browser can parse: show it as written.
  }

  return (
    <NodeViewWrapper contentEditable={false} className="my-2" data-selected={selected || undefined}>
      <a
        href={attrs.url}
        target="_blank"
        rel="noopener noreferrer"
        draggable={false}
        className="border-border bg-card hover:bg-muted/60 flex overflow-hidden rounded-md border no-underline in-data-selected:ring-2 in-data-selected:ring-ring"
      >
        <span className="flex min-w-0 flex-1 flex-col gap-1 p-3">
          <span className="text-foreground truncate text-sm font-medium">
            {attrs.title || attrs.url}
          </span>
          {attrs.description ? (
            <span className="text-muted-foreground line-clamp-2 text-xs">{attrs.description}</span>
          ) : null}
          <span className="text-muted-foreground flex min-w-0 items-center gap-1 text-xs">
            <GlobeIcon className="size-3 shrink-0" />
            <span className="truncate">{host}</span>
          </span>
        </span>
        {thumbnail ? (
          <img
            src={thumbnail}
            alt=""
            draggable={false}
            className="hidden w-40 shrink-0 object-cover sm:block"
          />
        ) : null}
      </a>
    </NodeViewWrapper>
  );
}
