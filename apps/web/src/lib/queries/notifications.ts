import { useEffect } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import {
  ApiError,
  getNotifications,
  getUnreadNotificationCount,
  markNotificationsRead,
  notificationStreamUrl,
  type AppNotification,
  type NotificationPage,
} from "@/lib/api";
import { connectEventStream } from "@/lib/event-stream";
import { keys } from "./keys";

type NotificationPages = InfiniteData<NotificationPage, string | undefined>;

/**
 * The unread badge. Live updates arrive through `useNotificationStream`;
 * the refetch on focus covers a tab whose stream was down while it slept.
 */
export function useUnreadNotificationCount() {
  return useQuery({
    queryKey: keys.notificationUnread,
    queryFn: getUnreadNotificationCount,
    staleTime: 60_000,
  });
}

/** The inbox, newest first, a page at a time. Only fetched while `enabled` (the popover is open). */
export function useNotifications(enabled: boolean) {
  return useInfiniteQuery({
    queryKey: keys.notificationList,
    queryFn: ({ pageParam }) => getNotifications(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled,
  });
}

/**
 * Marks notifications read — some, or `"all"`. The cache is updated before
 * the server answers so the badge and the dots drop on click; the answer
 * then replaces the guessed count with the server's.
 */
export function useMarkNotificationsRead() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[] | "all") => markNotificationsRead(ids),
    onMutate: async (ids) => {
      await qc.cancelQueries({ queryKey: keys.notifications });
      const now = new Date().toISOString();
      let newlyRead = 0;
      qc.setQueryData<NotificationPages>(keys.notificationList, (data) =>
        data
          ? {
              ...data,
              pages: data.pages.map((page) => ({
                ...page,
                items: page.items.map((item) => {
                  if (item.read_at || (ids !== "all" && !ids.includes(item.id))) return item;
                  newlyRead += 1;
                  return { ...item, read_at: now };
                }),
              })),
            }
          : data,
      );
      qc.setQueryData<number>(keys.notificationUnread, (count) =>
        count === undefined ? count : ids === "all" ? 0 : Math.max(0, count - newlyRead),
      );
    },
    onSuccess: (count) => qc.setQueryData(keys.notificationUnread, count),
    onSettled: () => qc.invalidateQueries({ queryKey: keys.notificationList }),
  });
}

/**
 * Keeps the inbox live while mounted: every `notification` event bumps the
 * badge, lands on top of a loaded list, and refreshes the comment threads of
 * its document so an open editor shows the new comment without waiting for
 * its poll. After a reconnect the inbox is refetched — events sent while the
 * stream was down are not replayed. Before reconnecting, the unread count is
 * refetched as a probe: the stream cannot report why it ended, and a 401 or
 * 403 there means the session is gone (the global 401 handler takes over),
 * so the stream stops instead of retrying forever.
 *
 * Mount it once per page; the notifications bell does.
 */
export function useNotificationStream() {
  const qc = useQueryClient();
  useEffect(
    () =>
      connectEventStream(notificationStreamUrl(), {
        handlers: {
          notification: (data) => {
            let item: AppNotification;
            try {
              item = JSON.parse(data) as AppNotification;
            } catch {
              return;
            }
            qc.setQueryData<number>(keys.notificationUnread, (count) =>
              count === undefined ? count : count + 1,
            );
            qc.setQueryData<NotificationPages>(keys.notificationList, (pages) => {
              if (!pages || pages.pages.length === 0) return pages;
              const [first, ...rest] = pages.pages;
              if (first.items.some((existing) => existing.id === item.id)) return pages;
              return { ...pages, pages: [{ ...first, items: [item, ...first.items] }, ...rest] };
            });
            if (item.thread_id)
              void qc.invalidateQueries({ queryKey: keys.comments(item.node.id) });
          },
        },
        canReconnect: () =>
          qc
            .fetchQuery({ queryKey: keys.notificationUnread, queryFn: getUnreadNotificationCount })
            .then(
              () => true,
              (err: unknown) => {
                if (err instanceof ApiError && (err.status === 401 || err.status === 403)) {
                  return false;
                }
                throw err;
              },
            ),
        onOpen: (reconnected) => {
          if (reconnected) void qc.invalidateQueries({ queryKey: keys.notifications });
        },
      }),
    [qc],
  );
}
