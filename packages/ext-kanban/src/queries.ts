// TanStack Query hooks for a kanban board: the snapshot, the optimistic
// mutations, and the people search. The snapshot lives under one key and the SSE
// stream (`use-board-events.ts`) keeps patching it, so a mutation's own response,
// the event it caused and an event from another tab all go through `applyEvent`.

import { useCallback, useEffect, useRef } from "react";
import { useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError, extQueryKey, extRetry } from "@pp-hub/ext-sdk";
import manifest from "../pp-app.json";
import * as kanban from "./api";
import type { Board, BoardEvent, Card, ColumnPatch } from "./api";
import {
  applyEvent,
  linksNeedRefetch,
  moveCardLocal,
  moveColumnLocal,
  patchCardLocal,
  type CardDrop,
  type LocalCardPatch,
} from "./board-state";

// eslint-disable-next-line lingui/no-unlocalized-strings -- cache key
export const boardKey = (nodeId: string) => extQueryKey(manifest.id, "board", nodeId);

export function useBoard(nodeId: string) {
  return useQuery({
    queryKey: boardKey(nodeId),
    queryFn: () => kanban.getBoard(nodeId),
    retry: extRetry,
  });
}

/** Applies a server event to the cached snapshot; `reload` refetches it. */
export function useApplyBoardEvent(nodeId: string) {
  const qc = useQueryClient();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  return useCallback(
    (event: BoardEvent) => {
      const key = boardKey(nodeId);
      const board = qc.getQueryData<Board>(key);
      if (!board) return;
      const next = applyEvent(board, event);
      if (next) qc.setQueryData<Board>(key, next);
      else void qc.invalidateQueries({ queryKey: key });
      // An event carries link ids but never names; resolve them with a snapshot,
      // batching a burst of events into one request.
      if (next && linksNeedRefetch(board, event)) {
        clearTimeout(timer.current);
        timer.current = setTimeout(
          () => void qc.invalidateQueries({ queryKey: key }),
          LINK_REFETCH_MS,
        );
      }
    },
    [qc, nodeId],
  );
}

const LINK_REFETCH_MS = 400;

/**
 * The cache plumbing every mutation shares: an optimistic edit with a
 * rollback, and a failure toast. A stale `if_rev` (409) refetches and says the
 * card changed instead of showing the server's sentence.
 */
function useBoardCache(nodeId: string) {
  const qc = useQueryClient();
  const { t } = useLingui();
  const key = boardKey(nodeId);

  const optimistic = async (edit: (board: Board) => Board) => {
    await qc.cancelQueries({ queryKey: key });
    const previous = qc.getQueryData<Board>(key);
    if (previous) qc.setQueryData<Board>(key, edit(previous));
    return { previous };
  };

  const rollback = (previous: Board | undefined) => {
    if (previous) qc.setQueryData<Board>(key, previous);
  };

  const fail = (err: Error, message: string) => {
    void qc.invalidateQueries({ queryKey: key });
    if (err instanceof ApiError && err.status === 409) {
      toast.error(t`This card was changed by someone else`, {
        description: t`The board has been refreshed. Review it and try again.`,
      });
      return;
    }
    toast.error(message, { description: err.message });
  };

  return { qc, key, optimistic, rollback, fail };
}

export function useMoveCard(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { optimistic, rollback, fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: ({ cardId, drop }: { cardId: string; drop: CardDrop }) =>
      kanban.moveCard(nodeId, cardId, drop.column_id, drop.after_id),
    onMutate: ({ cardId, drop }) =>
      optimistic((b) => moveCardLocal(b, cardId, drop.column_id, drop.after_id)),
    onSuccess: (card) => apply({ type: "card.upsert", card }),
    onError: (err, _vars, ctx) => {
      rollback(ctx?.previous);
      fail(err, t`Could not move the card`);
    },
  });
}

export function useUpdateCard(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { optimistic, rollback, fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: ({ card, patch }: { card: Card; patch: LocalCardPatch }) =>
      kanban.updateCard(nodeId, card.id, card.rev, {
        ...patch,
        assignees: patch.assignees?.map((p) => p.id),
        links: patch.links?.map((l) => l.id),
      }),
    onMutate: ({ card, patch }) => optimistic((b) => patchCardLocal(b, card.id, patch)),
    onSuccess: (card) => apply({ type: "card.upsert", card }),
    onError: (err, _vars, ctx) => {
      rollback(ctx?.previous);
      fail(err, t`Could not save the card`);
    },
  });
}

export function useCreateCard(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: (body: { column_id: string; title: string }) => kanban.createCard(nodeId, body),
    onSuccess: (card) => apply({ type: "card.upsert", card }),
    onError: (err) => fail(err, t`Could not add the card`),
  });
}

export function useDeleteCard(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { optimistic, rollback, fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: (cardId: string) => kanban.deleteCard(nodeId, cardId),
    onMutate: (cardId) =>
      optimistic((b) => applyEvent(b, { type: "card.delete", id: cardId }) ?? b),
    onSuccess: (_void, cardId) => apply({ type: "card.delete", id: cardId }),
    onError: (err, _id, ctx) => {
      rollback(ctx?.previous);
      fail(err, t`Could not delete the card`);
    },
  });
}

export function useCreateColumn(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: (body: { title: string; wip_limit?: number | null }) =>
      kanban.createColumn(nodeId, body),
    onSuccess: (column) => apply({ type: "column.upsert", column }),
    onError: (err) => fail(err, t`Could not add the column`),
  });
}

export function useUpdateColumn(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { optimistic, rollback, fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: ({ columnId, patch }: { columnId: string; patch: ColumnPatch }) =>
      kanban.updateColumn(nodeId, columnId, patch),
    onMutate: ({ columnId, patch }) =>
      optimistic((b) => {
        const renamed = {
          ...b,
          columns: b.columns.map((c) =>
            c.id === columnId
              ? {
                  ...c,
                  ...(patch.title !== undefined ? { title: patch.title } : {}),
                  ...(patch.wip_limit !== undefined ? { wip_limit: patch.wip_limit } : {}),
                }
              : c,
          ),
        };
        return patch.after_id === undefined
          ? renamed
          : moveColumnLocal(renamed, columnId, patch.after_id);
      }),
    onSuccess: (column) => apply({ type: "column.upsert", column }),
    onError: (err, _vars, ctx) => {
      rollback(ctx?.previous);
      fail(err, t`Could not update the column`);
    },
  });
}

/** Deleting a column renumbers cards on the server, which publishes `reload`. */
export function useDeleteColumn(nodeId: string) {
  const { t } = useLingui();
  const { qc, key, fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: ({ columnId, moveTo }: { columnId: string; moveTo?: string }) =>
      kanban.deleteColumn(nodeId, columnId, moveTo),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    onError: (err) => fail(err, t`Could not delete the column`),
  });
}

export function useCreateLabel(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: (body: { name: string; color: string }) => kanban.createLabel(nodeId, body),
    onSuccess: (label) => apply({ type: "label.upsert", label }),
    onError: (err) => fail(err, t`Could not create the label`),
  });
}

export function useUpdateLabel(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: ({
      labelId,
      patch,
    }: {
      labelId: string;
      patch: { name?: string; color?: string };
    }) => kanban.updateLabel(nodeId, labelId, patch),
    onSuccess: (label) => apply({ type: "label.upsert", label }),
    onError: (err) => fail(err, t`Could not update the label`),
  });
}

export function useDeleteLabel(nodeId: string) {
  const { t } = useLingui();
  const apply = useApplyBoardEvent(nodeId);
  const { fail } = useBoardCache(nodeId);
  return useMutation({
    mutationFn: (labelId: string) => kanban.deleteLabel(nodeId, labelId),
    onSuccess: (_void, id) => apply({ type: "label.delete", id }),
    onError: (err) => fail(err, t`Could not delete the label`),
  });
}

/** People who can open the board whose name or email matches `q`. */
export function usePeople(nodeId: string, q: string, enabled: boolean) {
  return useQuery({
    // eslint-disable-next-line lingui/no-unlocalized-strings -- cache key
    queryKey: extQueryKey(manifest.id, "people", nodeId, q),
    queryFn: () => kanban.searchPeople(nodeId, q),
    enabled,
    staleTime: 30_000,
  });
}
