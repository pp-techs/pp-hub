import { useMemo, useState } from "react";
import type { Editor } from "@tiptap/core";
import { useComments } from "@slash-editor/react";
import { useQuery } from "@tanstack/react-query";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Button } from "@workspace/ui/components/button";
import { Popover, PopoverContent } from "@workspace/ui/components/popover";
import type { PpCommentStore } from "@/lib/comments";
import { mentionedIds, type PickedMention } from "@/lib/comment-mentions";
import { commentsOptions, useSpaceMembers } from "@/lib/queries";
import { CommentInput } from "@/components/comments/comment-input";

/**
 * Starts a thread on the selected text. The editor's bubble "Comment" button
 * calls `openCommentComposer()`; this popover then floats at the selection,
 * creates the thread on the server (which picks its id), anchors it with a
 * `comment` mark and hands the id to `onCreated` so the panel can open on it.
 *
 * It also carries the comment marks' styling, so mount it for every viewer
 * of the document, readers included: an open thread's text is highlighted,
 * the one under the caret more strongly, and a resolved or deleted thread's
 * text not at all.
 */
export function CommentComposer({
  editor,
  store,
  spaceId,
  onCreated,
}: {
  editor: Editor;
  store: PpCommentStore;
  spaceId: string;
  onCreated?: (threadId: string) => void;
}) {
  const { t } = useLingui();
  const comments = useComments(editor);
  const members = useSpaceMembers(comments.composer.open ? spaceId : null);
  const [body, setBody] = useState("");
  const [picked, setPicked] = useState<PickedMention[]>([]);
  const [pending, setPending] = useState(false);

  const close = () => {
    setBody("");
    setPicked([]);
    comments.composer.close();
  };

  const submit = async () => {
    const text = body.trim();
    if (!text || pending) return;
    setPending(true);
    try {
      const thread = await store.createThread({ body: text, mentions: mentionedIds(text, picked) });
      editor.chain().focus().setComment(thread.id).run();
      close();
      onCreated?.(thread.id);
    } catch (err) {
      toast.error(t`Could not add the comment`, {
        description: err instanceof Error ? err.message : undefined,
      });
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <CommentMarkStyles nodeId={store.nodeId} activeThreadIds={comments.activeThreadIds} />
      <Popover
        open={comments.composer.open}
        onOpenChange={(open) => {
          if (!open) close();
        }}
      >
        <PopoverContent
          anchor={comments.composer.anchor ?? undefined}
          align="start"
          side="bottom"
          sideOffset={8}
          className="w-80 gap-2"
        >
          <CommentInput
            autoFocus
            value={body}
            onChange={setBody}
            onSubmit={() => void submit()}
            onCancel={close}
            onMention={(mention) => setPicked((prev) => [...prev, mention])}
            members={members.data}
            placeholder={t`Add a comment… (@ to mention)`}
            disabled={pending}
          />
          <div className="flex justify-end gap-1">
            <Button variant="ghost" size="sm" onClick={close}>
              <Trans>Cancel</Trans>
            </Button>
            <Button size="sm" disabled={!body.trim() || pending} onClick={() => void submit()}>
              <Trans>Comment</Trans>
            </Button>
          </div>
        </PopoverContent>
      </Popover>
    </>
  );
}

/**
 * Highlights the text of open threads. The mark itself carries only a
 * thread id, so whether a thread is open — or still exists — is known here
 * and not in the document; plain `comment` marks stay unstyled, which also
 * keeps them invisible in read-only renderings.
 */
function CommentMarkStyles({
  nodeId,
  activeThreadIds,
}: {
  nodeId: string;
  activeThreadIds: string[];
}) {
  const threads = useQuery(commentsOptions(nodeId));

  /* eslint-disable lingui/no-unlocalized-strings -- CSS rules */
  const css = useMemo(() => {
    const open = (threads.data ?? [])
      .filter((thread) => !thread.resolved)
      .map((thread) => thread.id);
    if (open.length === 0) return "";
    const active = activeThreadIds.filter((id) => open.includes(id));
    const select = (ids: string[]) =>
      `.slash-content span[data-type="comment"]:is(${ids
        .map((id) => `[data-thread-id="${CSS.escape(id)}"]`)
        .join(",")})`;
    const rules = [
      `${select(open)}{background-color:oklch(0.85 0.15 85 / 0.25);border-bottom:2px solid oklch(0.78 0.16 75 / 0.7);}`,
    ];
    if (active.length > 0) {
      rules.push(`${select(active)}{background-color:oklch(0.85 0.15 85 / 0.5);}`);
    }
    return rules.join("\n");
  }, [threads.data, activeThreadIds]);
  /* eslint-enable lingui/no-unlocalized-strings */

  return css ? <style>{css}</style> : null;
}
