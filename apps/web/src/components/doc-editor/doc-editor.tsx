import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Trans, useLingui } from "@lingui/react/macro";
import { msg } from "@lingui/core/macro";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { slashCommand, type PagesOptions, type SlashItem } from "@slash-editor/core";
import { EditorContent, useSlashEditor } from "@slash-editor/react";
import { useQueryClient } from "@tanstack/react-query";
import type * as Y from "yjs";
import type { Awareness } from "y-protocols/awareness";
import {
  Check,
  CloudOff,
  ListTree,
  Loader2,
  Lock,
  MessageSquareText,
  RefreshCw,
  SquarePen,
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
import { embedRelative, type Node } from "@/lib/api";
import { openFile } from "@/lib/open-file";
import { attachmentAdapter } from "@/lib/collab/attachments";
import { nodeDocumentFiles, relativeLinkClick } from "@/lib/collab/document-files";
import { createMdocKit } from "@/lib/collab/mdoc-kit";
import { mentionItems } from "@/lib/collab/mentions";
import { ROOM_META, usePageStore, type PpPageStore } from "@/lib/collab/page-store";
import { presenceColor } from "@/lib/collab/presence";
import { useCollabRoom, useSyncState } from "@/lib/collab/use-collab-room";
import { useCommentStore, type PpCommentStore } from "@/lib/comments";
import { useMe, useNode, usePageLineage } from "@/lib/queries";
import { LoadingStatus } from "@/components/loading";
import { EditorHeader } from "@/components/editor-header";
import { usePageCrumbs } from "@/components/page-trail";
import { BlockHandle } from "@/components/block-handle";
import { BubbleToolbar } from "@/components/bubble-toolbar";
import { LinkEditorPopover } from "@/components/link-editor-popover";
import { MentionMenu } from "@/components/mention-menu";
import { EmojiMenu } from "@/components/emoji-menu";
import { PresenceAvatars } from "@/components/presence-avatars";
import { SlashMenu } from "@/components/slash-menu";
import { TableControls } from "@/components/table-controls";
import { CommentComposer } from "@/components/comments/comment-composer";
import { BlockComments } from "@/components/comments/block-comments";
import { CommentsPopover } from "@/components/comments/comments-popover";
import { pickEmbedLink } from "@/components/nodes/embed-node-view";
import {
  MDOC_NODE_VIEW_KIT_OPTIONS,
  PAGE_NODE_VIEWS,
  mdocNodeViews,
} from "@/components/nodes/mdoc-node-views";
import {
  labelToggleButton,
  localizedPlaceholder,
  localizedSlashItems,
} from "@/components/doc-editor/labels";
import { LossyEditModal } from "@/components/doc-editor/lossy-edit-modal";
import { OutlinePanel } from "@/components/doc-editor/outline-panel";
import { PageBacklinks } from "@/components/doc-editor/page-backlinks";
import { PageHeader } from "@/components/doc-editor/page-header";
import { PageTreePanel } from "@/components/doc-editor/page-tree-panel";
import { useUiStore, type DocPanels } from "@/lib/ui-store";

const SLASH_HINT = msg`Type to search`;
const TRASH_FAILED = msg`Could not move the sub-page to the trash`;
const RESTORE_FAILED = msg`Could not restore the sub-page`;
const CREATE_FAILED = msg`Could not create the page`;

function errorMessage(error: unknown): string | undefined {
  return error instanceof Error ? error.message : undefined;
}

/** The page tree beside the document, and the comments popover's state. */
interface Panels extends DocPanels {
  comments: boolean;
  /** The thread the comments panel scrolls to and selects. */
  focusThreadId: string | null;
}

/** A narrow screen lays a side panel over the document, so it opens on demand only. */
const CLOSED_PANELS: DocPanels = { pages: false };

/**
 * The block editor for a markdown document: the shared header, and a
 * slash-editor surface co-edited over the document's collab room, with the
 * outline floating over it and the comments floating from the header and
 * from the blocks that have them.
 *
 * Whether this user may type comes from the room's REST answer
 * (`can_write`), never from the socket — the server drops a reader's writes
 * silently, and a reader left editable would keep them in a local copy that
 * never converges. A lossy file additionally opens read-only until the
 * writer confirms that saving will rewrite it.
 *
 * `thread` (the page's `?thread=`, where a comment notification links)
 * opens the comments on that thread.
 *
 * `onSettled` is called once the page has something to show in place of a
 * skeleton: the editor is built, or the room was refused or failed. A page
 * loading out of sight is put on screen then.
 */
export function DocEditor({
  node,
  thread,
  onSettled,
}: {
  node: Node;
  thread?: string;
  onSettled?: () => void;
}) {
  const { t } = useLingui();
  const { room, retry } = useCollabRoom(node.id);
  const commentStore = useCommentStore(node.id);
  // eslint-disable-next-line lingui/no-unlocalized-strings -- source discriminant
  const pageSource = { kind: "node", spaceId: node.space_id } as const;
  const pageStore = usePageStore(pageSource);
  const lineage = usePageLineage(pageSource, node.id);
  const ancestors = lineage.data?.ancestors ?? [];
  const rootPage = useNode(ancestors[0]?.id);
  const [lossyAccepted, setLossyAccepted] = useState(false);
  // The page tree follows the reader across pages (each page remounts this
  // editor) and reloads; on a narrow screen it would cover the document, so
  // there it starts closed and is this page's alone.
  const isMobile = useIsMobile();
  const docPanels = useUiStore((s) => s.docPanels);
  const setDocPanel = useUiStore((s) => s.setDocPanel);
  const [mobilePanels, setMobilePanels] = useState<DocPanels>(CLOSED_PANELS);
  const [commentsButton, setCommentsButton] = useState<HTMLElement | null>(null);
  const [threadPanel, setThreadPanel] = useState({
    comments: !!thread,
    focusThreadId: thread ?? null,
  });
  const panels: Panels = { ...(isMobile ? mobilePanels : docPanels), ...threadPanel };
  const setPanels = (update: (panels: Panels) => Panels) => {
    const next = update(panels);
    // eslint-disable-next-line lingui/no-unlocalized-strings -- DocPanels keys
    for (const key of ["pages"] as const) {
      if (next[key] === panels[key]) continue;
      if (isMobile) setMobilePanels((p) => ({ ...p, [key]: next[key] }));
      else setDocPanel(key, next[key]);
    }
    setThreadPanel({ comments: next.comments, focusThreadId: next.focusThreadId });
  };
  // The editor latches its page options once, so this must not change.
  const openPage = useCallback(
    (pageId: string) => openFile({ id: pageId, space_id: node.space_id }),
    [node.space_id],
  );
  const pageCrumbs = usePageCrumbs(pageStore, node.id, openPage);
  // One listing names every sub-page block's page, before each block asks
  // for its own.
  useEffect(() => {
    Promise.resolve(pageStore.listChildren(node.id)).catch(() => undefined);
  }, [pageStore, node.id]);
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

  useEffect(() => {
    if (room.phase === "denied" || room.phase === "failed") onSettled?.();
  }, [room.phase, onSettled]);

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
      <EditorHeader
        nodeId={node.id}
        node={node}
        crumbs={pageCrumbs.ancestors}
        // A sub-page's way back to the Drive is its top-level page's folder
        // (`null`: the space's top level).
        driveDir={ancestors.length > 0 ? (rootPage.data?.parent_id ?? null) : undefined}
      >
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
              label={t`Pages`}
              pressed={panels.pages}
              onPressedChange={(pages) => setPanels((p) => ({ ...p, pages }))}
            >
              <ListTree />
            </PanelToggle>
            <PanelToggle
              label={t`Comments`}
              pressed={panels.comments}
              onPressedChange={(comments) =>
                setPanels((p) => ({ ...p, comments, focusThreadId: null }))
              }
              toggleRef={setCommentsButton}
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
              pageStore={pageStore}
              onNavigate={openPage}
              onShown={onSettled}
              // The tree hangs from the top-most page this reader can view.
              treeRootId={ancestors[0]?.id ?? node.id}
              treeExpanded={ancestors.map((page) => page.id)}
              treeReady={!lineage.isPending}
              commentsButton={commentsButton}
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
  pageStore,
  onNavigate,
  treeRootId,
  treeExpanded,
  treeReady,
  commentsButton,
  onShown,
}: {
  node: Node;
  doc: Y.Doc;
  /** Anything carrying the room's awareness: carets and presence read it. */
  provider: { awareness: Awareness };
  editable: boolean;
  commentStore: PpCommentStore;
  panels: Panels;
  onPanelsChange: (update: (panels: Panels) => Panels) => void;
  pageStore: PpPageStore;
  /** Opens a page; stable, since the kit latches it. */
  onNavigate: (pageId: string) => void;
  /** The page the tree hangs from, and the pages it starts with open. */
  treeRootId: string;
  treeExpanded: string[];
  /** The open page's ancestors are known, so the tree can open on it. */
  treeReady: boolean;
  /** The header's Comments button, which the global comments popover hangs from. */
  commentsButton: HTMLElement | null;
  onShown?: () => void;
}) {
  const [openThreadId, setOpenThreadId] = useState<string | null>(null);
  const { t, i18n } = useLingui();
  const me = useMe();
  const queryClient = useQueryClient();
  const isMobile = useIsMobile();
  const scrollRef = useRef<HTMLDivElement>(null);
  const documentId = node.id;
  const name = me.data?.display_name || me.data?.name || me.data?.email || t`Anonymous`;
  const color = presenceColor(me.data?.id ?? "");
  const adapter = useMemo(() => attachmentAdapter(documentId), [documentId]);
  // Latched by the kit with the rest; `onNavigate` is stable.
  const files = useMemo(
    () => nodeDocumentFiles(documentId, { openPage: onNavigate }),
    [documentId, onNavigate],
  );

  // The room's `meta` map is the open page's live icon and cover.
  useEffect(
    () => pageStore.bindMeta(documentId, doc.getMap(ROOM_META)),
    [pageStore, documentId, doc],
  );

  // Latched with the rest of the kit's options on the first render. A
  // deleted sub-page block trashes its page, an undo or paste brings it
  // back; a refusal (a co-editor without the right to delete it, say) leaves
  // the page in the tree and says so.
  const pages: PagesOptions = {
    store: pageStore,
    currentPageId: documentId,
    onNavigate,
    onSubPagesDetached: (pageIds) => {
      for (const pageId of pageIds) {
        pageStore.trash(pageId).catch((error: unknown) => {
          toast.error(i18n._(TRASH_FAILED), { description: errorMessage(error) });
        });
      }
    },
    onSubPagesAttached: (pageIds) => {
      for (const pageId of pageIds) {
        pageStore.restore(pageId).catch((error: unknown) => {
          toast.error(i18n._(RESTORE_FAILED), { description: errorMessage(error) });
        });
      }
    },
    onError: (error) => {
      toast.error(i18n._(CREATE_FAILED), { description: errorMessage(error) });
    },
    nodeViews: PAGE_NODE_VIEWS,
  };

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
          files,
          editing: { documentId, adapter, embed: (path) => embedRelative(documentId, path) },
          mentionItems: mentionItems(queryClient, node.id),
          pages,
        }),
      ],
      pages,
    }),
  });

  useEffect(() => {
    editor?.setEditable(editable);
  }, [editor, editable]);

  // A frame after the editor exists, not at once: the page tree and header
  // beside it fill from the cache in microtasks, and a page put on screen
  // before they have would show them empty for a frame.
  useEffect(() => {
    if (!editor) return;
    const frame = requestAnimationFrame(() => onShown?.());
    return () => cancelAnimationFrame(frame);
  }, [editor, onShown]);

  if (!editor) {
    return (
      <div className="min-h-0 flex-1 overflow-y-auto">
        <DocSkeleton />
      </div>
    );
  }

  return (
    <>
      {panels.pages ? (
        <SidePanel side="left">
          <PageTreePanel
            // The tree opens on the page once its ancestors are known.
            key={treeReady ? treeRootId : null}
            store={pageStore}
            rootId={treeRootId}
            activeId={documentId}
            expanded={treeExpanded}
            editable={editable}
            onNavigate={onNavigate}
            onCreate={
              editable
                ? // eslint-disable-next-line lingui/no-unlocalized-strings -- focus position
                  () => editor.chain().focus("end").createSubPage().run()
                : undefined
            }
            onClose={() => onPanelsChange((p) => ({ ...p, pages: false }))}
          />
        </SidePanel>
      ) : null}
      {/* min-w-0: without it the surface cannot shrink below the document's
          min-content width (wide tables, the md gutters), so on a narrow window
          it overflows the right edge and takes the outline anchored there with
          it. */}
      <div className="relative min-h-0 min-w-0 flex-1">
        <div ref={scrollRef} className="h-full overflow-y-auto">
          <div className="mx-auto w-full max-w-5xl px-4 pt-10 md:pr-32 md:pl-20">
            <PageHeader
              store={pageStore}
              pageId={documentId}
              editor={editor}
              editable={editable}
              adapter={adapter}
            />
          </div>
          {/* A relative link (`[spec](../Spec.md)`) is the drive's path, not
              this page's: opened through the document, never by the browser. */}
          <div
            className="slash-content relative mx-auto w-full max-w-5xl px-4 pt-4 pb-10 md:pr-32 md:pl-20"
            onClickCapture={relativeLinkClick(files, () => editor.isEditable)}
          >
            <EditorContent editor={editor} />
            {editable ? (
              <>
                <SlashMenu editor={editor} />
                <MentionMenu editor={editor} />
                <EmojiMenu editor={editor} />
                <BubbleToolbar editor={editor} />
                <LinkEditorPopover editor={editor} openRelative={files.openRelative} />
                <TableControls editor={editor} />
                {/* The handle sits in a gutter left of the text that a narrow
                  screen does not have; there, blocks move by cut and paste. */}
                {isMobile ? null : <BlockHandle editor={editor} />}
              </>
            ) : null}
            <CommentComposer editor={editor} store={commentStore} onCreated={setOpenThreadId} />
            <BlockComments
              editor={editor}
              store={commentStore}
              openThreadId={openThreadId}
              onOpenThreadChange={setOpenThreadId}
            />
          </div>
          <div className="mx-auto w-full max-w-5xl px-4 pb-10 md:pr-32 md:pl-20">
            <PageBacklinks store={pageStore} pageId={documentId} onNavigate={onNavigate} />
          </div>
        </div>
        <OutlinePanel
          editor={editor}
          scrollRef={scrollRef}
          className="absolute top-4 right-4 z-10"
        />
      </div>
      <CommentsPopover
        editor={editor}
        store={commentStore}
        open={panels.comments}
        onOpenChange={(comments) =>
          onPanelsChange((p) => ({ ...p, comments, focusThreadId: null }))
        }
        anchor={commentsButton}
        focusThreadId={panels.focusThreadId}
      />
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

/** A header button that shows or hides one of the side panels or floating views. */
function PanelToggle({
  label,
  pressed,
  onPressedChange,
  toggleRef,
  children,
}: {
  label: string;
  pressed: boolean;
  onPressedChange: (pressed: boolean) => void;
  /** The button's element, for a view that hangs from it. */
  toggleRef?: (el: HTMLButtonElement | null) => void;
  children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Toggle
            ref={toggleRef}
            aria-label={label}
            pressed={pressed}
            onPressedChange={onPressedChange}
          >
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
