import { useMemo } from "react";
import { hashKey, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { CommentThread, CommentThreadStore } from "@slash-editor/core";
import {
  createCommentThread,
  deleteComment,
  editComment,
  replyToThread,
  setThreadResolved,
  type DocCommentThread,
} from "@/lib/api";
import { commentsOptions } from "@/lib/queries";

/** A new comment: the text, and the users it @mentions (ids, notified by the server). */
export interface CommentInput {
  body: string;
  mentions?: string[];
}

/** What deleting a comment did: the server drops a thread with its last comment. */
export interface DeletedComment {
  threadId: string | null;
  threadDeleted: boolean;
}

/**
 * The editor's `CommentThreadStore` for one markdown document, over the REST
 * API and the TanStack cache under `keys.comments(nodeId)`. The document's
 * `comment` marks carry only thread ids; the threads live on the server.
 *
 * Beyond slash-editor's interface it takes `mentions` and edits/deletes
 * single comments, which the comments panel needs. Every write puts the
 * server's answer in the cache at once and then refetches, and every cache
 * change for the document notifies subscribers — whether it came from a
 * write here, the poll, or a live notification.
 */
export interface PpCommentStore extends CommentThreadStore {
  readonly nodeId: string;
  createThread(input: CommentInput): Promise<CommentThread>;
  addMessage(threadId: string, input: CommentInput): Promise<CommentThread>;
  resolveThread(threadId: string): Promise<void>;
  reopenThread(threadId: string): Promise<void>;
  getThread(threadId: string): Promise<CommentThread | undefined>;
  listThreads(): Promise<CommentThread[]>;
  editComment(commentId: string, body: string): Promise<void>;
  deleteComment(commentId: string): Promise<DeletedComment>;
}

/** The slash-editor view of a server thread. */
export function toEditorThread(thread: DocCommentThread): CommentThread {
  return {
    id: thread.id,
    // eslint-disable-next-line lingui/no-unlocalized-strings -- slash-editor thread status values
    status: thread.resolved ? "resolved" : "open",
    messages: thread.comments.map((comment) => ({
      id: comment.id,
      author: comment.author.name,
      body: comment.body,
      createdAt: Date.parse(comment.created_at),
    })),
  };
}

export function createCommentStore(
  qc: QueryClient,
  nodeId: string,
  subject?: string,
): PpCommentStore {
  const options = commentsOptions(nodeId, subject);
  const queryHash = hashKey(options.queryKey);

  const threads = () => qc.ensureQueryData(options);
  const refresh = () => qc.invalidateQueries({ queryKey: options.queryKey });

  /** Puts one thread as the server answered it into the cache (or removes it). */
  const settle = (threadId: string, next: DocCommentThread | null) => {
    qc.setQueryData<DocCommentThread[]>(options.queryKey, (current = []) => {
      const rest = current.filter((thread) => thread.id !== threadId);
      if (!next) return rest;
      const index = current.findIndex((thread) => thread.id === threadId);
      if (index === -1) return [...current, next];
      return current.map((thread) => (thread.id === threadId ? next : thread));
    });
    void refresh();
  };

  return {
    nodeId,

    async createThread({ body, mentions }) {
      const thread = await createCommentThread(nodeId, body, mentions, subject);
      settle(thread.id, thread);
      return toEditorThread(thread);
    },

    async addMessage(threadId, { body, mentions }) {
      const thread = await replyToThread(threadId, body, mentions);
      settle(threadId, thread);
      return toEditorThread(thread);
    },

    async resolveThread(threadId) {
      settle(threadId, await setThreadResolved(threadId, true));
    },

    async reopenThread(threadId) {
      settle(threadId, await setThreadResolved(threadId, false));
    },

    async getThread(threadId) {
      const found = (await threads()).find((thread) => thread.id === threadId);
      return found ? toEditorThread(found) : undefined;
    },

    async listThreads() {
      return (await threads()).map(toEditorThread);
    },

    async editComment(commentId, body) {
      const comment = await editComment(commentId, body);
      qc.setQueryData<DocCommentThread[]>(options.queryKey, (current) =>
        current?.map((thread) => ({
          ...thread,
          comments: thread.comments.map((c) => (c.id === comment.id ? comment : c)),
        })),
      );
      void refresh();
    },

    async deleteComment(commentId) {
      const owner = qc
        .getQueryData<DocCommentThread[]>(options.queryKey)
        ?.find((thread) => thread.comments.some((c) => c.id === commentId));
      await deleteComment(commentId);
      if (!owner) {
        void refresh();
        return { threadId: null, threadDeleted: false };
      }
      const remaining = owner.comments.filter((c) => c.id !== commentId);
      const threadDeleted = remaining.length === 0;
      settle(owner.id, threadDeleted ? null : { ...owner, comments: remaining });
      return { threadId: owner.id, threadDeleted };
    },

    subscribe(listener) {
      return qc.getQueryCache().subscribe((event) => {
        if (
          event.query.queryHash === queryHash &&
          event.type === "updated" &&
          event.action.type === "success"
        ) {
          listener();
        }
      });
    },
  };
}

/**
 * The comment store of the document open in this editor, stable for its
 * `nodeId` — create it before the editor, which binds it once. While the
 * calling component is mounted the threads are polled every 10 s (and on
 * window focus), so comments from other people arrive even without a live
 * notification for them.
 */
export function useCommentStore(nodeId: string): PpCommentStore {
  const qc = useQueryClient();
  const store = useMemo(() => createCommentStore(qc, nodeId), [qc, nodeId]);
  useQuery({ ...commentsOptions(nodeId), refetchInterval: 10_000, refetchOnWindowFocus: true });
  return store;
}

/**
 * The discussion of one subject of an extension node ("card:<id>"): its
 * threads (oldest first, resolved included), polled every 10 s while the
 * caller is mounted, and the store whose `createThread`, `addMessage`,
 * `resolveThread`, `reopenThread`, `editComment` and `deleteComment` write
 * them. New threads carry `subject`.
 */
export function useNodeThreads(nodeId: string, subject: string) {
  const qc = useQueryClient();
  const store = useMemo(() => createCommentStore(qc, nodeId, subject), [qc, nodeId, subject]);
  const query = useQuery({
    ...commentsOptions(nodeId, subject),
    refetchInterval: 10_000,
    refetchOnWindowFocus: true,
  });
  return { threads: query.data, isLoading: query.isLoading, error: query.error, store };
}
