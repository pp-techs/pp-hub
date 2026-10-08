import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useLingui } from "@lingui/react/macro";
import { extQueryKey, extRetry, jsonEvent, useExtEvents } from "@pp-hub/ext-sdk";
import manifest from "../pp-app.json";
import * as checklist from "./api";
import type { List, ListEvent } from "./api";
import { applyEvent, upsertItem } from "./list-state";

// eslint-disable-next-line lingui/no-unlocalized-strings -- cache key
const listKey = (nodeId: string) => extQueryKey(manifest.id, "list", nodeId);

export function useList(nodeId: string) {
  return useQuery({
    queryKey: listKey(nodeId),
    queryFn: () => checklist.getList(nodeId),
    retry: extRetry,
  });
}

/** Keeps the list live while mounted; a reconnect refetches what the stream missed. */
export function useListEvents(nodeId: string) {
  const qc = useQueryClient();
  useExtEvents({
    url: checklist.eventsUrl(nodeId),
    handlers: {
      list: jsonEvent<ListEvent>((event) =>
        qc.setQueryData<List>(listKey(nodeId), (list) => list && applyEvent(list, event)),
      ),
    },
    refetch: () =>
      qc.fetchQuery({
        queryKey: listKey(nodeId),
        queryFn: () => checklist.getList(nodeId),
        staleTime: 0,
      }),
  });
}

/** Add, tick, rename and delete; each puts the server's answer into the cache. */
export function useListMutations(nodeId: string) {
  const qc = useQueryClient();
  const { t } = useLingui();
  const key = listKey(nodeId);
  const onError = (err: Error) => {
    void qc.invalidateQueries({ queryKey: key });
    toast.error(t`Could not update the checklist`, { description: err.message });
  };
  const put = (item: checklist.Item) =>
    qc.setQueryData<List>(key, (list) => list && { ...list, items: upsertItem(list.items, item) });

  return {
    add: useMutation({
      mutationFn: (text: string) => checklist.createItem(nodeId, text),
      onSuccess: put,
      onError,
    }),
    update: useMutation({
      mutationFn: (v: { id: string; text?: string; done?: boolean }) =>
        checklist.updateItem(nodeId, v.id, { text: v.text, done: v.done }),
      // Ticking is the common act: show it at once, roll back on failure.
      onMutate: async (v) => {
        await qc.cancelQueries({ queryKey: key });
        const previous = qc.getQueryData<List>(key);
        qc.setQueryData<List>(
          key,
          (list) =>
            list && {
              ...list,
              items: list.items.map((i) =>
                i.id === v.id ? { ...i, text: v.text ?? i.text, done: v.done ?? i.done } : i,
              ),
            },
        );
        return { previous };
      },
      onError: (err, _v, ctx) => {
        if (ctx?.previous) qc.setQueryData<List>(key, ctx.previous);
        onError(err);
      },
      onSuccess: put,
    }),
    remove: useMutation({
      mutationFn: (id: string) => checklist.deleteItem(nodeId, id),
      onSuccess: (_r, id) =>
        qc.setQueryData<List>(key, (list) => list && applyEvent(list, { type: "item.delete", id })),
      onError,
    }),
  };
}
