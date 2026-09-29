import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { slashCommand, type SlashItem } from "@slash-editor/core";
import { EditorContent, useSlashEditor } from "@slash-editor/react";
import { useQueryClient } from "@tanstack/react-query";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import {
  Check,
  CloudOff,
  Loader2,
  Lock,
  MessageSquareText,
  RefreshCw,
  SquarePen,
  TableOfContents,
} from "lucide-react";
import { useIsMobile } from "@workspace/ui/hooks/use-mobile";
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
import { Toggle } from "@workspace/ui/components/toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@workspace/ui/components/tooltip";
import { attachmentUrl, type Node } from "@/lib/api";
import { attachmentAdapter } from "@/lib/collab/attachments";
import { createMdocKit } from "@/lib/collab/mdoc-kit";
import { mentionItems } from "@/lib/collab/mentions";
import { presenceColor } from "@/lib/collab/presence";
import { useCollabRoom, useSyncState } from "@/lib/collab/use-collab-room";
import { useCommentStore, type PpCommentStore } from "@/lib/comments";
import { useMe } from "@/lib/queries";
import { LoadingStatus } from "@/components/loading";
import { EditorHeader } from "@/components/editor-header";
import { BLOCK_GUTTER_WIDTH, BlockHandle } from "@/components/block-handle";
import { BubbleToolbar } from "@/components/bubble-toolbar";
import { LinkEditorPopover } from "@/components/link-editor-popover";
import { MentionMenu } from "@/components/mention-menu";
import { PresenceAvatars } from "@/components/presence-avatars";
import { SlashMenu } from "@/components/slash-menu";
import { TableControls } from "@/components/table-controls";
import { CommentComposer } from "@/components/comments/comment-composer";
import { CommentsPanel } from "@/components/comments/comments-panel";
import { pickEmbedLink } from "@/components/nodes/embed-node-view";
import { MDOC_NODE_VIEW_KIT_OPTIONS, mdocNodeViews } from "@/components/nodes/mdoc-node-views";
import {
  labelToggleButton,
  localizedPlaceholder,
  localizedSlashItems,
} from "@/components/doc-editor/labels";
import { LossyEditModal } from "@/components/doc-editor/lossy-edit-modal";
import { OutlinePanel } from "@/components/doc-editor/outline-panel";

const SLASH_HINT = msg`Type to search`;

/** The side panels beside the document; both may be open at once. */
interface Panels {
  outline: boolean;
  comments: boolean;
  /** The thread the comments panel scrolls to and selects. */
  focusThreadId: string | null;
}

/**
 * The block editor for a markdown document: the shared header, and a
 * slash-editor surface co-edited over the document's collab room, with the
 * outline and the comment threads as panels beside it.
 *
 * Whether this user may type comes from the room's REST answer
 * (`can_write`), never from the socket — the server drops a reader's writes
 * silently, and a reader left editable would keep them in a local copy that
 * never converges. A lossy file additionally opens read-only until the
 * writer confirms that saving will rewrite it.
 *
 * `thread` (the page's `?thread=`, where a comment notification links)
 * opens the comments on that thread.
 */
export function DocEditor({ node, thread }: { node: Node; thread?: string }) {
  const { t } = useLingui();
  const { room, retry } = useCollabRoom(node.id);
  const commentStore = useCommentStore(node.id);
  const [lossyAccepted, setLossyAccepted] = useState(false);
  const [panels, setPanels] = useState<Panels>({
    outline: false,
    comments: !!thread,
    focusThreadId: thread ?? null,
  });
  // A notification followed while the document is already open changes only
  // the search: open the panel on the new thread, during render rather than
  // in an effect so the first paint already shows it.
  const [seenThread, setSeenThread] = useState(thread);
  if (thread !== seenThread) {
    setSeenThread(thread);
    if (thread) setPanels((p) => ({ ...p, comments: true, focusThreadId: thread }));
  }

  const session = room.phase === "open" ? room.session : null;
  const lossyLocked = !!session?.lossy && !lossyAccepted;
  const editable = !!session?.can_write && !lossyLocked;
  const ready = room.phase === "open" && room.ready;

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
        {ready ? (
          <>
            <PanelToggle
              label={t`Outline`}
              pressed={panels.outline}
              onPressedChange={(outline) => setPanels((p) => ({ ...p, outline }))}
            >
              <TableOfContents />
            </PanelToggle>
            <PanelToggle
              label={t`Comments`}
              pressed={panels.comments}
              onPressedChange={(comments) =>
                setPanels((p) => ({ ...p, comments, focusThreadId: null }))
              }
            >
              <MessageSquareText />
            </PanelToggle>
          </>
        ) : null}
      </EditorHeader>

      <div className="relative flex min-h-0 flex-1">
        {room.phase === "open" && room.ready ? (
          <TooltipProvider>
            <DocSurface
              // A new room is a new Y.Doc: the editor binds its document once.
              key={room.session.room}
              node={node}
              doc={room.doc}
              provider={room}
              editable={editable}
              commentStore={commentStore}
              panels={panels}
              onPanelsChange={setPanels}
            />
          </TooltipProvider>
        ) : (
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
                      Your access may have been removed, or the file is no longer a markdown
                      document.
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
                    {room.error instanceof Error
                      ? room.error.message
                      : t`The server did not answer.`}
                  </EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button variant="outline" size="sm" onClick={retry}>
                    <RefreshCw data-icon="inline-start" />
                    <Trans>Try again</Trans>
                  </Button>
                </EmptyContent>
              </Empty>
            ) : (
              <DocSkeleton />
            )}
          </div>
        )}
      </div>
    </>
  );
}

function DocSurface({
  node,
  doc,
  provider,
  editable,
  commentStore,
  panels,
  onPanelsChange,
}: {
  node: Node;
  doc: Y.Doc;
  /** Anything carrying the room's awareness: carets and presence read it. */
  provider: { awareness: Awareness };
  editable: boolean;
  commentStore: PpCommentStore;
  panels: Panels;
  onPanelsChange: (update: (panels: Panels) => Panels) => void;
}) {
  const { t, i18n } = useLingui();
  const me = useMe();
  const queryClient = useQueryClient();
  const isMobile = useIsMobile();
  const scrollRef = useRef<HTMLDivElement>(null);
  const documentId = node.id;
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
      ...MDOC_NODE_VIEW_KIT_OPTIONS,
      collaboration: { document: doc, provider, user: { name, color } },
      comment: { store: commentStore },
      slash: false,
      drag: { gutterWidth: BLOCK_GUTTER_WIDTH },
      tableOfContents: { maxLevel: 3 },
      placeholder: { resolve: (context) => localizedPlaceholder(i18n, context) },
      toggle: {
        renderToggleButton: ({ element, isOpen }) => labelToggleButton(i18n, element, isOpen),
      },
      extend: [
        slashCommand({
          items: () => localizedSlashItems(i18n, { embed: insertEmbed(documentId) }),
          hint: i18n._(SLASH_HINT),
        }),
        ...mdocNodeViews({
          attachmentUrl: (hash) => attachmentUrl(documentId, hash),
          editing: { documentId, adapter },
          mentionItems: mentionItems(queryClient, node.space_id),
        }),
      ],
    }),
  });

  useEffect(() => {
    editor?.setEditable(editable);
  }, [editor, editable]);

  if (!editor) {
    return (
      <div className="min-h-0 flex-1 overflow-y-auto">
        <DocSkeleton />
      </div>
    );
  }

  return (
    <>
      {panels.outline ? (
        <SidePanel side="left">
          <OutlinePanel
            editor={editor}
            scrollRef={scrollRef}
            onClose={() => onPanelsChange((p) => ({ ...p, outline: false }))}
          />
        </SidePanel>
      ) : null}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <div className="slash-content mx-auto w-full max-w-5xl px-4 py-10 md:px-20">
          <EditorContent editor={editor} />
          {editable ? (
            <>
              <SlashMenu editor={editor} />
              <MentionMenu editor={editor} />
              <BubbleToolbar editor={editor} />
              <LinkEditorPopover editor={editor} />
              <TableControls editor={editor} />
              {/* The handle sits in a gutter left of the text that a narrow
                  screen does not have; there, blocks move by cut and paste. */}
              {isMobile ? null : <BlockHandle editor={editor} />}
            </>
          ) : null}
          <CommentComposer
            editor={editor}
            store={commentStore}
            spaceId={node.space_id}
            onCreated={(threadId) =>
              onPanelsChange((p) => ({ ...p, comments: true, focusThreadId: threadId }))
            }
          />
        </div>
      </div>
      {panels.comments ? (
        <SidePanel side="right">
          <CommentsPanel
            editor={editor}
            store={commentStore}
            spaceId={node.space_id}
            focusThreadId={panels.focusThreadId}
            onClose={() => onPanelsChange((p) => ({ ...p, comments: false, focusThreadId: null }))}
          />
        </SidePanel>
      ) : null}
    </>
  );
}

/**
 * The slash item for a link card: an empty embed block goes in where the
 * `/` was, and the link dialog fills it — or takes it out again when
 * dismissed. The block carries its own id so the dialog finds it after any
 * edits made meanwhile.
 */
function insertEmbed(documentId: string): SlashItem["run"] {
  return ({ editor, range }) => {
    const id = crypto.randomUUID();
    editor.chain().focus().deleteRange(range).insertContent({ type: "embed", attrs: { id } }).run();
    pickEmbedLink(editor, documentId, id, { removeIfCancelled: true });
  };
}

/**
 * A panel beside the document. Below `md` there is no room beside it, so it
 * lies over the document's edge instead.
 */
function SidePanel({ side, children }: { side: "left" | "right"; children: ReactNode }) {
  return (
    <div
      data-side={side}
      className="bg-background absolute inset-y-0 z-10 flex shadow-lg data-[side=left]:left-0 data-[side=right]:right-0 md:static md:shadow-none"
    >
      {children}
    </div>
  );
}

/** A header button that shows or hides one of the side panels. */
function PanelToggle({
  label,
  pressed,
  onPressedChange,
  children,
}: {
  label: string;
  pressed: boolean;
  onPressedChange: (pressed: boolean) => void;
  children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Toggle aria-label={label} pressed={pressed} onPressedChange={onPressedChange}>
            {children}
          </Toggle>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
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
    <LoadingStatus className="mx-auto flex w-full max-w-5xl flex-col gap-3 px-4 py-10 md:px-20">
      <Skeleton className="h-7 w-1/2" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-11/12" />
      <Skeleton className="h-4 w-4/5" />
      <Skeleton className="h-4 w-2/3" />
    </LoadingStatus>
  );
}
