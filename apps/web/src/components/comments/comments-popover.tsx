import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import type { Editor } from "@tiptap/core";
import type { Node as PmNode } from "@tiptap/pm/model";
import { useQuery } from "@tanstack/react-query";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import {
  Check,
  MessageSquare,
  MoreHorizontal,
  Pencil,
  RotateCcw,
  Trash2,
  Unlink,
} from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu";
import { Label } from "@workspace/ui/components/label";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { Switch } from "@workspace/ui/components/switch";
import { cn } from "@workspace/ui/lib/utils";
import { capsOf, type DocComment, type DocCommentThread, type NodePerson } from "@/lib/api";
import type { PpCommentStore } from "@/lib/comments";
import { mentionedIds, type PickedMention } from "@/lib/comment-mentions";
import { useFormatters } from "@/lib/format";
import { commentsOptions, useMe, useNode, useNodePeople } from "@/lib/queries";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { LoadingStatus } from "@/components/loading";
import { CommentInput } from "@/components/comments/comment-input";
import { SocialAvatar } from "@/components/comments/social-avatar";

/** Where a thread's `comment` mark sits in the document, and the text it covers. */
export interface ThreadAnchor {
  from: number;
  to: number;
  /** Position before the text block the mark starts in. */
  block: number;
  text: string;
}

/**
 * Every thread anchored in `doc`, in document order. A thread's anchor runs
 * from the first to the last character carrying its mark — the mark may be
 * split by other formatting, and overlapping threads each get their own.
 */
function collectAnchors(doc: PmNode): Map<string, ThreadAnchor> {
  const spans = new Map<string, { from: number; to: number }>();
  doc.descendants((node, pos) => {
    if (!node.isText) return;
    for (const mark of node.marks) {
      if (mark.type.name !== "comment") continue;
      const id = mark.attrs.threadId as string | null;
      if (!id) continue;
      const span = spans.get(id);
      const end = pos + node.nodeSize;
      if (span) span.to = Math.max(span.to, end);
      else spans.set(id, { from: pos, to: end });
    }
  });
  const anchors = new Map<string, ThreadAnchor>();
  for (const [id, { from, to }] of spans) {
    const $from = doc.resolve(from);
    anchors.set(id, {
      from,
      to,
      block: $from.depth > 0 ? $from.before() : from,
      text: doc.textBetween(from, to, " "),
    });
  }
  return anchors;
}

/** The anchors of the live document, recomputed on every change (local or remote). */
export function useThreadAnchors(editor: Editor): Map<string, ThreadAnchor> {
  const [anchors, setAnchors] = useState(() => collectAnchors(editor.state.doc));
  useEffect(() => {
    const update = () => setAnchors(collectAnchors(editor.state.doc));
    update();
    // eslint-disable-next-line lingui/no-unlocalized-strings -- Tiptap event name
    editor.on("update", update);
    return () => {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- Tiptap event name
      editor.off("update", update);
    };
  }, [editor]);
  return anchors;
}

/**
 * What a thread card needs from the editor and the document's people, shared
 * by the global comments popover and the per-block ones: the anchors, the
 * text selection a card jumps to, and the props every `ThreadCard` takes.
 */
export function useThreadCards(editor: Editor, store: PpCommentStore) {
  const node = useNode(store.nodeId);
  const me = useMe();
  const members = useNodePeople(store.nodeId);
  const anchors = useThreadAnchors(editor);
  const canModerate = capsOf(node.data).edit;

  const selectText = (threadId: string, focus: boolean) => {
    const anchor = anchors.get(threadId);
    if (!anchor) return;
    const chain = editor.chain();
    if (focus) chain.focus();
    chain.setTextSelection({ from: anchor.from, to: anchor.to }).scrollIntoView().run();
  };

  /** A deleted thread's mark goes with it, when this viewer may write the document. */
  const dropAnchor = (threadId: string) => {
    if (!editor.isEditable) return;
    editor
      .chain()
      .command(({ tr, state }) => {
        const type = state.schema.marks.comment;
        if (!type) return false;
        tr.removeMark(0, state.doc.content.size, type.create({ threadId }));
        return true;
      })
      .run();
  };

  const cardProps = (thread: DocCommentThread) => ({
    thread,
    anchor: anchors.get(thread.id),
    meId: me.data?.id,
    canModerate,
    store,
    members: members.data,
    onThreadDeleted: () => dropAnchor(thread.id),
  });

  return { anchors, selectText, cardProps };
}

/**
 * Every thread of the open document, floating from the header's Comments
 * button. Threads whose mark is in the document are listed in document
 * order; clicking one selects its text. Threads whose text was deleted are
 * "detached" and listed after. Resolved threads are hidden unless asked for.
 *
 * `focusThreadId` (the `?thread=` deep link from a notification) selects that
 * thread once its data has loaded, showing resolved threads if it is one.
 */
export function CommentsPopover({
  editor,
  store,
  open,
  onOpenChange,
  anchor,
  focusThreadId,
}: {
  editor: Editor;
  store: PpCommentStore;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The header button the popover hangs from. */
  anchor: HTMLElement | null;
  focusThreadId?: string | null;
}) {
  const { t } = useLingui();
  return (
    <Popover
      open={open}
      onOpenChange={(next, details) => {
        // The header button opens and closes this itself; pressing it is an
        // outside press here, and closing on it would let its own click reopen.
        if (
          !next &&
          details.reason === "outside-press" &&
          details.event.target instanceof Node &&
          anchor?.contains(details.event.target)
        ) {
          return;
        }
        onOpenChange(next);
      }}
    >
      <PopoverContent
        anchor={anchor}
        align="end"
        side="bottom"
        sideOffset={8}
        aria-label={t`Comments`}
        className="max-h-[min(80svh,40rem)] w-96 max-w-[calc(100vw-1rem)] gap-0 p-0"
      >
        <CommentsList editor={editor} store={store} focusThreadId={focusThreadId} />
      </PopoverContent>
    </Popover>
  );
}

function CommentsList({
  editor,
  store,
  focusThreadId,
}: {
  editor: Editor;
  store: PpCommentStore;
  focusThreadId?: string | null;
}) {
  const { t } = useLingui();
  const threads = useQuery(commentsOptions(store.nodeId));
  const { anchors, selectText, cardProps } = useThreadCards(editor, store);
  const activeIds = useSyncExternalStore(
    (listener) => editor.storage.comment.subscribe(listener),
    () => editor.storage.comment.state.activeThreadIds,
  );

  const [showResolved, setShowResolved] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const cards = useRef(new Map<string, HTMLElement>());
  const handledFocus = useRef<string | null>(null);

  // The caret entering a thread's text selects that thread here too — unless
  // the selected one is among those under the caret (overlapping threads).
  useEffect(() => {
    const first = activeIds[0];
    if (first && !activeIds.includes(selectedId ?? "")) setSelectedId(first);
  }, [activeIds, selectedId]);

  const select = (threadId: string) => {
    setSelectedId(threadId);
    selectText(threadId, true);
  };

  useEffect(() => {
    if (!focusThreadId || handledFocus.current === focusThreadId || !threads.data) return;
    handledFocus.current = focusThreadId;
    const target = threads.data.find((thread) => thread.id === focusThreadId);
    if (!target) {
      toast.info(t`That comment thread no longer exists.`);
      return;
    }
    if (target.resolved) setShowResolved(true);
    setSelectedId(target.id);
    selectText(target.id, false);
    requestAnimationFrame(() =>
      // eslint-disable-next-line lingui/no-unlocalized-strings -- scroll options
      cards.current.get(target.id)?.scrollIntoView({ block: "nearest", behavior: "smooth" }),
    );
    // Once per deep link; `anchors` changing later must not re-select.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusThreadId, threads.data]);

  const { anchored, detached, resolvedCount } = useMemo(() => {
    const all = threads.data ?? [];
    const visible = all.filter((thread) => showResolved || !thread.resolved);
    const order = [...anchors.keys()];
    return {
      anchored: visible
        .filter((thread) => anchors.has(thread.id))
        .sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id)),
      detached: visible
        .filter((thread) => !anchors.has(thread.id))
        .sort((a, b) => a.created_at.localeCompare(b.created_at)),
      resolvedCount: all.filter((thread) => thread.resolved).length,
    };
  }, [threads.data, anchors, showResolved]);

  const renderCard = (thread: DocCommentThread) => (
    <ThreadCard
      key={thread.id}
      ref={(el) => {
        if (el) cards.current.set(thread.id, el);
        else cards.current.delete(thread.id);
      }}
      {...cardProps(thread)}
      selected={thread.id === selectedId}
      onSelect={() => select(thread.id)}
    />
  );

  const empty = anchored.length === 0 && detached.length === 0;

  return (
    <>
      <div className="border-border flex h-10 shrink-0 items-center gap-2 border-b px-3">
        <span className="text-sm font-medium">
          <Trans>Comments</Trans>
        </span>
        <Label className="text-muted-foreground ml-auto flex items-center gap-1.5 text-[11px] font-normal">
          <Switch size="sm" checked={showResolved} onCheckedChange={setShowResolved} />
          <Trans>Resolved ({resolvedCount})</Trans>
        </Label>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-2">
        {threads.isPending ? (
          <LoadingStatus className="flex flex-col gap-2">
            {[0, 1].map((i) => (
              <Skeleton key={i} className="h-24 rounded-lg" />
            ))}
          </LoadingStatus>
        ) : threads.isError ? (
          <p className="text-destructive p-2 text-xs">{threads.error.message}</p>
        ) : empty ? (
          <div className="text-muted-foreground flex flex-col items-center gap-2 px-4 py-10 text-center text-xs">
            <MessageSquare className="size-5" />
            {resolvedCount > 0 && !showResolved ? (
              <Trans>No open comments.</Trans>
            ) : editor.isEditable ? (
              <Trans>No comments yet. Select some text and choose Comment to start a thread.</Trans>
            ) : (
              <Trans>No comments yet.</Trans>
            )}
          </div>
        ) : (
          <>
            {anchored.map(renderCard)}
            {detached.length > 0 ? (
              <>
                <div className="text-muted-foreground mt-2 flex items-center gap-1.5 px-1 text-[11px] font-medium">
                  <Unlink className="size-3" />
                  <Trans>Detached — the commented text was removed</Trans>
                </div>
                {detached.map(renderCard)}
              </>
            ) : null}
          </>
        )}
      </div>
    </>
  );
}

export function ThreadCard({
  ref,
  thread,
  anchor,
  selected,
  onSelect,
  meId,
  canModerate,
  store,
  members,
  onThreadDeleted,
}: {
  ref?: (el: HTMLElement | null) => void;
  thread: DocCommentThread;
  anchor: ThreadAnchor | undefined;
  selected: boolean;
  onSelect: () => void;
  meId: string | undefined;
  canModerate: boolean;
  store: PpCommentStore;
  members: NodePerson[] | undefined;
  onThreadDeleted: () => void;
}) {
  const { t } = useLingui();
  const [reply, setReply] = useState("");
  const [picked, setPicked] = useState<PickedMention[]>([]);
  const [pending, setPending] = useState(false);
  const [deleting, setDeleting] = useState<DocComment | null>(null);

  const canResolve = canModerate || thread.created_by.id === meId;

  const run = async (action: () => Promise<unknown>, failure: string) => {
    setPending(true);
    try {
      await action();
      return true;
    } catch (err) {
      toast.error(failure, { description: err instanceof Error ? err.message : undefined });
      return false;
    } finally {
      setPending(false);
    }
  };

  const sendReply = async () => {
    const body = reply.trim();
    if (!body) return;
    const ok = await run(
      () => store.addMessage(thread.id, { body, mentions: mentionedIds(body, picked) }),
      t`Could not send the reply`,
    );
    if (ok) {
      setReply("");
      setPicked([]);
    }
  };

  const toggleResolved = () =>
    void run(
      () => (thread.resolved ? store.reopenThread(thread.id) : store.resolveThread(thread.id)),
      thread.resolved ? t`Could not reopen the thread` : t`Could not resolve the thread`,
    );

  const confirmDelete = async (comment: DocComment) => {
    await run(
      async () => {
        const result = await store.deleteComment(comment.id);
        if (result.threadDeleted) onThreadDeleted();
      },
      t`Could not delete the comment`,
    );
  };

  return (
    <section
      ref={ref}
      aria-label={t`Comment thread`}
      data-thread-id={thread.id}
      className={cn(
        "bg-card ring-foreground/10 flex flex-col gap-2 rounded-lg p-2.5 text-xs ring-1 transition-shadow",
        selected && "ring-primary/60 shadow-sm",
        thread.resolved && "opacity-75",
      )}
    >
      <div className="flex items-start gap-2">
        <button
          type="button"
          className="text-muted-foreground min-w-0 flex-1 cursor-pointer text-left"
          onClick={onSelect}
        >
          {anchor ? (
            <span className="border-l-2 border-amber-400/70 line-clamp-2 pl-2 italic">
              {anchor.text}
            </span>
          ) : (
            <span className="flex items-center gap-1 text-[11px]">
              <Unlink className="size-3" />
              <Trans>Detached</Trans>
            </span>
          )}
        </button>
        {thread.resolved ? (
          <Badge variant="secondary" className="shrink-0 text-[10px]">
            <Trans>Resolved</Trans>
          </Badge>
        ) : null}
        {canResolve ? (
          <Button
            variant="ghost"
            size="icon-xs"
            disabled={pending}
            aria-label={thread.resolved ? t`Reopen thread` : t`Resolve thread`}
            title={thread.resolved ? t`Reopen thread` : t`Resolve thread`}
            onClick={toggleResolved}
          >
            {thread.resolved ? <RotateCcw /> : <Check />}
          </Button>
        ) : null}
      </div>

      <ol className="flex flex-col gap-2.5">
        {thread.comments.map((comment) => (
          <CommentItem
            key={comment.id}
            comment={comment}
            store={store}
            canEdit={comment.author.id === meId}
            canDelete={comment.author.id === meId || canModerate}
            onDelete={() => setDeleting(comment)}
          />
        ))}
      </ol>

      {selected && !thread.resolved ? (
        <div className="flex flex-col gap-1.5">
          <CommentInput
            value={reply}
            onChange={setReply}
            onSubmit={() => void sendReply()}
            onMention={(mention) => setPicked((prev) => [...prev, mention])}
            members={members}
            placeholder={t`Reply… (@ to mention)`}
            disabled={pending}
          />
          {reply.trim() ? (
            <div className="flex justify-end gap-1">
              <Button variant="ghost" size="xs" onClick={() => setReply("")}>
                <Trans>Cancel</Trans>
              </Button>
              <Button size="xs" disabled={pending} onClick={() => void sendReply()}>
                <Trans>Reply</Trans>
              </Button>
            </div>
          ) : null}
        </div>
      ) : !selected && (!thread.resolved || anchor) ? (
        <button
          type="button"
          className="text-muted-foreground hover:text-foreground cursor-pointer self-start text-[11px]"
          onClick={onSelect}
        >
          {thread.resolved ? <Trans>Show in document</Trans> : <Trans>Reply</Trans>}
        </button>
      ) : null}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        title={t`Delete this comment?`}
        description={
          thread.comments.length === 1
            ? t`It is the only comment in its thread, so the thread is deleted too.`
            : t`The rest of the thread stays.`
        }
        confirmLabel={t`Delete`}
        onConfirm={() => {
          if (deleting) void confirmDelete(deleting);
          setDeleting(null);
        }}
      />
    </section>
  );
}

function CommentItem({
  comment,
  store,
  canEdit,
  canDelete,
  onDelete,
}: {
  comment: DocComment;
  store: PpCommentStore;
  canEdit: boolean;
  canDelete: boolean;
  onDelete: () => void;
}) {
  const { t } = useLingui();
  const f = useFormatters();
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const save = async () => {
    const body = draft?.trim();
    if (!body) return;
    if (body === comment.body) {
      setDraft(null);
      return;
    }
    setSaving(true);
    try {
      await store.editComment(comment.id, body);
      setDraft(null);
    } catch (err) {
      toast.error(t`Could not save the comment`, {
        description: err instanceof Error ? err.message : undefined,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <li className="group/comment flex gap-2">
      <SocialAvatar user={comment.author} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex items-center gap-1.5">
          <span className="truncate font-medium">{comment.author.name}</span>
          <time
            dateTime={comment.created_at}
            title={f.date(comment.created_at)}
            className="text-muted-foreground shrink-0 text-[10px]"
          >
            {f.relative(comment.created_at)}
          </time>
          {comment.edited_at ? (
            <span className="text-muted-foreground shrink-0 text-[10px]">
              <Trans>(edited)</Trans>
            </span>
          ) : null}
          {(canEdit || canDelete) && draft === null ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label={t`Comment actions`}
                    className="ml-auto opacity-0 group-hover/comment:opacity-100 focus-visible:opacity-100 data-popup-open:opacity-100"
                  />
                }
              >
                <MoreHorizontal />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-32">
                {canEdit ? (
                  <DropdownMenuItem onClick={() => setDraft(comment.body)}>
                    <Pencil />
                    <Trans>Edit</Trans>
                  </DropdownMenuItem>
                ) : null}
                {canDelete ? (
                  <DropdownMenuItem variant="destructive" onClick={onDelete}>
                    <Trash2 />
                    <Trans>Delete</Trans>
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>
        {draft !== null ? (
          <div className="flex flex-col gap-1.5">
            <CommentInput
              autoFocus
              value={draft}
              onChange={setDraft}
              onSubmit={() => void save()}
              onCancel={() => setDraft(null)}
              disabled={saving}
            />
            <div className="flex justify-end gap-1">
              <Button variant="ghost" size="xs" onClick={() => setDraft(null)}>
                <Trans>Cancel</Trans>
              </Button>
              <Button size="xs" disabled={saving || !draft.trim()} onClick={() => void save()}>
                <Trans>Save</Trans>
              </Button>
            </div>
          </div>
        ) : (
          <p className="break-words whitespace-pre-wrap">{comment.body}</p>
        )}
      </div>
    </li>
  );
}
