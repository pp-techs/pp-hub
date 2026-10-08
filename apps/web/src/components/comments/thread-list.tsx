import { useEffect, useRef, useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Check, RotateCcw } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import type { DocComment, DocCommentThread, NodePerson } from "@/lib/api";
import { mentionedIds, type PickedMention } from "@/lib/comment-mentions";
import { useNodeThreads, type PpCommentStore } from "@/lib/comments";
import { useMe, useNodePeople } from "@/lib/queries";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { CommentInput } from "@/components/comments/comment-input";
import { CommentItem } from "@/components/comments/comments-popover";

/**
 * The discussion of one subject of an extension node (a kanban card), for an
 * extension's own dialog: its threads with replies, resolve/reopen, edit and
 * delete of single comments, and a composer (with @mentions of the node's
 * people) that starts a new thread on `subject`.
 *
 * Props:
 * - `nodeId`  the extension node.
 * - `subject` the part of it, e.g. "card:<uuid>"; threads are listed and
 *   created under it. Polled every 10 s while mounted.
 * - `canComment` may this viewer write? Off: the thread list is read-only
 *   (no composer, no reply box, no edit/delete).
 * - `canResolve` may this viewer resolve/reopen and delete anyone's thread or
 *   comment (an editor of the node)? Off: only the thread's author may
 *   resolve it, and only comment authors may delete their own.
 * - `focusThreadId` optional: a thread to scroll to and highlight (the
 *   `?thread=` of a notification link).
 */
export function ThreadList({
  nodeId,
  subject,
  canComment,
  canResolve,
  focusThreadId,
  className,
}: {
  nodeId: string;
  subject: string;
  canComment: boolean;
  canResolve: boolean;
  focusThreadId?: string;
  className?: string;
}) {
  const { t } = useLingui();
  const { threads, isLoading, store } = useNodeThreads(nodeId, subject);
  const me = useMe();
  const members = useNodePeople(nodeId);
  const [body, setBody] = useState("");
  const [picked, setPicked] = useState<PickedMention[]>([]);
  const [pending, setPending] = useState(false);

  const startThread = async () => {
    const text = body.trim();
    if (!text) return;
    setPending(true);
    try {
      await store.createThread({ body: text, mentions: mentionedIds(text, picked) });
      setBody("");
      setPicked([]);
    } catch (err) {
      toast.error(t`Could not post the comment`, {
        description: err instanceof Error ? err.message : undefined,
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      {isLoading ? (
        <Skeleton className="h-16 w-full" />
      ) : threads && threads.length > 0 ? (
        threads.map((thread) => (
          <SubjectThread
            key={thread.id}
            thread={thread}
            store={store}
            meId={me.data?.id}
            canComment={canComment}
            canResolve={canResolve}
            members={members.data}
            focused={thread.id === focusThreadId}
          />
        ))
      ) : (
        <p className="text-muted-foreground text-xs">
          <Trans>No comments yet.</Trans>
        </p>
      )}

      {canComment ? (
        <div className="flex flex-col gap-1.5">
          <CommentInput
            value={body}
            onChange={setBody}
            onSubmit={() => void startThread()}
            onMention={(mention) => setPicked((prev) => [...prev, mention])}
            members={members.data}
            placeholder={t`Write a comment… (@ to mention)`}
            disabled={pending}
          />
          {body.trim() ? (
            <div className="flex justify-end">
              <Button size="xs" disabled={pending} onClick={() => void startThread()}>
                <Trans>Comment</Trans>
              </Button>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function SubjectThread({
  thread,
  store,
  meId,
  canComment,
  canResolve,
  members,
  focused,
}: {
  thread: DocCommentThread;
  store: PpCommentStore;
  meId: string | undefined;
  canComment: boolean;
  canResolve: boolean;
  members: NodePerson[] | undefined;
  focused: boolean;
}) {
  const { t } = useLingui();
  const ref = useRef<HTMLElement>(null);
  const [reply, setReply] = useState("");
  const [picked, setPicked] = useState<PickedMention[]>([]);
  const [pending, setPending] = useState(false);
  const [deleting, setDeleting] = useState<DocComment | null>(null);

  useEffect(() => {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM scroll option
    if (focused) ref.current?.scrollIntoView({ block: "nearest" });
  }, [focused]);

  const mayResolve = canComment && (canResolve || thread.created_by.id === meId);

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
    const text = reply.trim();
    if (!text) return;
    const ok = await run(
      () => store.addMessage(thread.id, { body: text, mentions: mentionedIds(text, picked) }),
      t`Could not send the reply`,
    );
    if (ok) {
      setReply("");
      setPicked([]);
    }
  };

  return (
    <section
      ref={ref}
      aria-label={t`Comment thread`}
      data-thread-id={thread.id}
      className={cn(
        "bg-card ring-foreground/10 flex flex-col gap-2 rounded-lg p-2.5 text-xs ring-1",
        focused && "ring-primary/60 shadow-sm",
        thread.resolved && "opacity-75",
      )}
    >
      {thread.resolved || mayResolve ? (
        <div className="flex items-center justify-end gap-2">
          {thread.resolved ? (
            <Badge variant="secondary" className="text-[10px]">
              <Trans>Resolved</Trans>
            </Badge>
          ) : null}
          {mayResolve ? (
            <Button
              variant="ghost"
              size="icon-xs"
              disabled={pending}
              aria-label={thread.resolved ? t`Reopen thread` : t`Resolve thread`}
              title={thread.resolved ? t`Reopen thread` : t`Resolve thread`}
              onClick={() =>
                void run(
                  () =>
                    thread.resolved
                      ? store.reopenThread(thread.id)
                      : store.resolveThread(thread.id),
                  thread.resolved
                    ? t`Could not reopen the thread`
                    : t`Could not resolve the thread`,
                )
              }
            >
              {thread.resolved ? <RotateCcw /> : <Check />}
            </Button>
          ) : null}
        </div>
      ) : null}

      <ol className="flex flex-col gap-2.5">
        {thread.comments.map((comment) => (
          <CommentItem
            key={comment.id}
            comment={comment}
            store={store}
            canEdit={canComment && comment.author.id === meId}
            canDelete={canComment && (comment.author.id === meId || canResolve)}
            onDelete={() => setDeleting(comment)}
          />
        ))}
      </ol>

      {canComment && !thread.resolved ? (
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
          const target = deleting;
          setDeleting(null);
          if (target)
            void run(() => store.deleteComment(target.id), t`Could not delete the comment`);
        }}
      />
    </section>
  );
}
