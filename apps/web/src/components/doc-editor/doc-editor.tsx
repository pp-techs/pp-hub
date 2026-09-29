import { useEffect, useMemo, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import {
  File as FileNode,
  Image,
  Mermaid,
  retryUpload,
  slashCommand,
  type UploadAdapter,
} from "@slash-editor/core";
import { EditorContent, useSlashEditor } from "@slash-editor/react";
import { ReactNodeViewRenderer } from "@tiptap/react";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import { Check, CloudOff, Loader2, Lock, RefreshCw, SquarePen } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { TooltipProvider } from "@workspace/ui/components/tooltip";
import type { Node } from "@/lib/api";
import { attachmentAdapter, type AttachmentResult } from "@/lib/collab/attachments";
import { createMdocKit } from "@/lib/collab/mdoc-kit";
import { presenceColor } from "@/lib/collab/presence";
import { useCollabRoom, useSyncState } from "@/lib/collab/use-collab-room";
import { useMe } from "@/lib/queries";
import { LoadingStatus } from "@/components/loading";
import { EditorHeader } from "@/components/editor-header";
import { BLOCK_GUTTER_WIDTH, BlockHandle } from "@/components/block-handle";
import { BubbleToolbar } from "@/components/bubble-toolbar";
import { LinkEditorPopover } from "@/components/link-editor-popover";
import { PresenceAvatars } from "@/components/presence-avatars";
import { SlashMenu } from "@/components/slash-menu";
import { FileNodeView } from "@/components/nodes/file-node-view";
import { ImageNodeView } from "@/components/nodes/image-node-view";
import { MermaidNodeView } from "@/components/nodes/mermaid-node-view";
import {
  labelToggleButton,
  localizedPlaceholder,
  localizedSlashItems,
} from "@/components/doc-editor/labels";
import { LossyEditModal } from "@/components/doc-editor/lossy-edit-modal";

const SLASH_HINT = msg`Type to search`;

/**
 * The block editor for a markdown document: the shared header, and a
 * slash-editor surface co-edited over the document's collab room.
 *
 * Whether this user may type comes from the room's REST answer
 * (`can_write`), never from the socket — the server drops a reader's writes
 * silently, and a reader left editable would keep them in a local copy that
 * never converges. A lossy file additionally opens read-only until the
 * writer confirms that saving will rewrite it.
 */
export function DocEditor({ node }: { node: Node }) {
  const { t } = useLingui();
  const { room, retry } = useCollabRoom(node.id);
  const [lossyAccepted, setLossyAccepted] = useState(false);

  const session = room.phase === "open" ? room.session : null;
  const lossyLocked = !!session?.lossy && !lossyAccepted;
  const editable = !!session?.can_write && !lossyLocked;

  const confirmLossyEdit = () => {
    LossyEditModal.show({ name: node.name }).then(
      (accepted) => {
        if (accepted) setLossyAccepted(true);
      },
      // Reopened or removed before an answer: nothing was decided.
      () => undefined,
    );
  };

  return (
    <>
      <EditorHeader nodeId={node.id} node={node}>
        {room.phase === "open" ? (
          <>
            <PresenceAvatars provider={room} />
            <SyncIndicator provider={room.provider} />
          </>
        ) : null}
        {session && !editable ? (
          <Badge variant="secondary">
            <Lock data-icon="inline-start" />
            <Trans>Read-only</Trans>
          </Badge>
        ) : null}
        {session?.can_write && lossyLocked ? (
          <Button variant="outline" size="sm" onClick={confirmLossyEdit}>
            <SquarePen data-icon="inline-start" />
            <Trans>Edit</Trans>
          </Button>
        ) : null}
      </EditorHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {room.phase === "denied" ? (
          <Empty className="h-full">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Lock />
              </EmptyMedia>
              <EmptyTitle>
                <Trans>This document is not available</Trans>
              </EmptyTitle>
              <EmptyDescription>
                <Trans>
                  Your access may have been removed, or the file is no longer a markdown document.
                </Trans>
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : room.phase === "failed" ? (
          <Empty className="h-full">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <CloudOff />
              </EmptyMedia>
              <EmptyTitle>
                <Trans>Could not open the document</Trans>
              </EmptyTitle>
              <EmptyDescription>
                {room.error instanceof Error ? room.error.message : t`The server did not answer.`}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button variant="outline" size="sm" onClick={retry}>
                <RefreshCw data-icon="inline-start" />
                <Trans>Try again</Trans>
              </Button>
            </EmptyContent>
          </Empty>
        ) : room.phase === "open" && room.ready ? (
          <TooltipProvider>
            <DocSurface
              // A new room is a new Y.Doc: the editor binds its document once.
              key={room.session.room}
              documentId={node.id}
              doc={room.doc}
              provider={room}
              editable={editable}
            />
          </TooltipProvider>
        ) : (
          <DocSkeleton />
        )}
      </div>
    </>
  );
}

function DocSurface({
  documentId,
  doc,
  provider,
  editable,
}: {
  documentId: string;
  doc: Y.Doc;
  /** Anything carrying the room's awareness: carets and presence read it. */
  provider: { awareness: Awareness };
  editable: boolean;
}) {
  const { t, i18n } = useLingui();
  const me = useMe();
  const name = me.data?.display_name || me.data?.name || me.data?.email || t`Anonymous`;
  const color = presenceColor(me.data?.id ?? "");
  const adapter = useMemo(() => attachmentAdapter(documentId), [documentId]);

  // Resolved once per editor instance (ProseMirror cannot swap a schema on a
  // live document). Copy is looked up through `i18n` when it is shown, so
  // the menus and placeholders still follow a language switch.
  const editor = useSlashEditor({
    editable,
    blockKit: false,
    extensions: createMdocKit({
      collaboration: { document: doc, provider, user: { name, color } },
      slash: false,
      image: false,
      drag: { gutterWidth: BLOCK_GUTTER_WIDTH },
      file: false,
      mermaid: false,
      placeholder: { resolve: (context) => localizedPlaceholder(i18n, context) },
      toggle: {
        renderToggleButton: ({ element, isOpen }) => labelToggleButton(i18n, element, isOpen),
      },
      extend: [
        slashCommand({ items: () => localizedSlashItems(i18n), hint: i18n._(SLASH_HINT) }),
        Image.extend({
          addNodeView: () =>
            ReactNodeViewRenderer((props) => (
              <ImageNodeView {...props} adapter={adapter} documentId={documentId} />
            )),
        }),
        attachmentFile(adapter).extend({
          addNodeView: () =>
            ReactNodeViewRenderer((props) => (
              <FileNodeView {...props} adapter={adapter} documentId={documentId} />
            )),
        }),
        Mermaid.extend({ addNodeView: () => ReactNodeViewRenderer(MermaidNodeView) }),
      ],
    }),
  });

  useEffect(() => {
    editor?.setEditable(editable);
  }, [editor, editable]);

  if (!editor) return <DocSkeleton />;

  return (
    <div className="slash-content mx-auto w-full max-w-5xl px-20 py-10">
      <EditorContent editor={editor} />
      {editable ? (
        <>
          <SlashMenu editor={editor} />
          <BubbleToolbar editor={editor} />
          <LinkEditorPopover editor={editor} />
          <BlockHandle editor={editor} />
        </>
      ) : null}
    </div>
  );
}

/**
 * The file node, with a retry that records what the upload was. Core's own
 * `retryFile` keeps only `src`, which leaves a file attached to an empty
 * placeholder nameless and sizeless; the adapter already knows all three.
 */
function attachmentFile(adapter: UploadAdapter<AttachmentResult>) {
  return FileNode.extend({
    addCommands() {
      return {
        ...this.parent?.(),
        retryFile:
          (id, override) =>
          ({ dispatch }) => {
            const hasEntry = override !== undefined || this.storage.pending.get(id) !== undefined;
            if (!dispatch || !hasEntry) return hasEntry;
            queueMicrotask(() => {
              retryUpload<AttachmentResult>({
                editor: this.editor,
                typeName: this.name,
                id,
                pending: this.storage.pending,
                toAttrs: (result) => ({
                  src: result.url,
                  name: result.name,
                  size: result.size,
                  mime: result.mime,
                }),
                override: override ? { file: override.file, adapter } : undefined,
              });
            });
            return true;
          },
      };
    },
  });
}

/** Where this tab's own edits are: acknowledged by the server, in flight, or waiting for a socket. */
function SyncIndicator({ provider }: { provider: HocuspocusProvider }) {
  const state = useSyncState(provider);
  return (
    <span
      role="status"
      data-state={state}
      className="text-muted-foreground flex items-center gap-1 px-1 text-xs"
    >
      {state === "saved" ? (
        <>
          <Check className="size-3.5" />
          <Trans>Saved</Trans>
        </>
      ) : state === "syncing" ? (
        <>
          <Loader2 className="size-3.5 animate-spin" />
          <Trans>Syncing…</Trans>
        </>
      ) : (
        <>
          <CloudOff className="size-3.5" />
          <Trans>Offline</Trans>
        </>
      )}
    </span>
  );
}

/** Stand-in for the document while the room loads: a title and paragraph lines. */
function DocSkeleton() {
  return (
    <LoadingStatus className="mx-auto flex w-full max-w-5xl flex-col gap-3 px-20 py-10">
      <Skeleton className="h-7 w-1/2" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-11/12" />
      <Skeleton className="h-4 w-4/5" />
      <Skeleton className="h-4 w-2/3" />
    </LoadingStatus>
  );
}
