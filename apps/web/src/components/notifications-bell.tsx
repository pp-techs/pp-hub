import { useState } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { Trans, useLingui } from "@lingui/react/macro";
import { Bell, CheckCheck, Circle, Loader2 } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import type { AppNotification } from "@/lib/api";
import { useFormatters } from "@/lib/format";
import {
  useMarkNotificationsRead,
  useNotificationStream,
  useNotifications,
  useUnreadNotificationCount,
} from "@/lib/queries";
import { LoadingStatus } from "@/components/loading";
import { SocialAvatar } from "@/components/comments/social-avatar";

/** Where a notification leads: its document, opened on its thread when it has one. */
function notificationHref(item: AppNotification): string {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
  const base = `/edit/${item.node.id}`;
  // eslint-disable-next-line lingui/no-unlocalized-strings -- URL query
  return item.thread_id ? `${base}?thread=${encodeURIComponent(item.thread_id)}` : base;
}

/**
 * The inbox: a bell with the unread count, opening the newest notifications.
 * Mounted once per page (the drive header or the editor header), and it holds
 * the page's live stream so the badge moves without a refresh.
 *
 * Opening one marks it read and goes to the document with the thread
 * focused: in the same tab from the editor, in a new tab from the drive —
 * the way every document opens from the drive.
 */
export function NotificationsBell() {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const pathname = useLocation({ select: (location) => location.pathname });
  useNotificationStream();
  const unread = useUnreadNotificationCount();
  const list = useNotifications(open);
  const markRead = useMarkNotificationsRead();

  const count = unread.data ?? 0;
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];

  const openItem = (item: AppNotification) => {
    if (!item.read_at) markRead.mutate([item.id]);
    setOpen(false);
    // eslint-disable-next-line lingui/no-unlocalized-strings -- URL path
    if (pathname.startsWith("/edit/")) {
      void navigate({
        to: "/edit/$nodeId",
        params: { nodeId: item.node.id },
        search: { thread: item.thread_id },
      });
    } else {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- window target
      window.open(notificationHref(item), "_blank");
    }
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            className="relative"
            aria-label={count > 0 ? t`Notifications, ${count} unread` : t`Notifications`}
          />
        }
      >
        <Bell />
        {count > 0 ? (
          <span
            aria-hidden="true"
            className="bg-destructive absolute -top-0.5 -right-0.5 flex h-3.5 min-w-3.5 items-center justify-center rounded-full px-1 text-[9px] leading-none font-semibold text-white tabular-nums"
          >
            {count > 99 ? "99+" : count}
          </span>
        ) : null}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-96 gap-0 p-0">
        <div className="border-border flex h-10 items-center gap-2 border-b px-3">
          <span className="text-sm font-medium">
            <Trans>Notifications</Trans>
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            disabled={count === 0 || markRead.isPending}
            // eslint-disable-next-line lingui/no-unlocalized-strings -- API argument
            onClick={() => markRead.mutate("all")}
          >
            <CheckCheck data-icon="inline-start" />
            <Trans>Mark all read</Trans>
          </Button>
        </div>

        <div className="max-h-[28rem] overflow-y-auto">
          {list.isPending ? (
            <LoadingStatus className="flex flex-col gap-3 p-3">
              {[0, 1, 2].map((i) => (
                <div key={i} className="flex gap-2">
                  <Skeleton className="size-6 shrink-0 rounded-full" />
                  <div className="flex flex-1 flex-col gap-1.5">
                    <Skeleton className="h-3 w-3/4" />
                    <Skeleton className="h-3 w-1/2" />
                  </div>
                </div>
              ))}
            </LoadingStatus>
          ) : list.isError ? (
            <p className="text-destructive p-3 text-xs">{list.error.message}</p>
          ) : items.length === 0 ? (
            <p className="text-muted-foreground px-3 py-8 text-center text-xs">
              <Trans>No notifications yet.</Trans>
            </p>
          ) : (
            <ul className="flex flex-col">
              {items.map((item) => (
                <NotificationRow
                  key={item.id}
                  item={item}
                  onOpen={() => openItem(item)}
                  onMarkRead={() => markRead.mutate([item.id])}
                />
              ))}
            </ul>
          )}
          {list.hasNextPage ? (
            <div className="border-border border-t p-1.5">
              <Button
                variant="ghost"
                size="sm"
                className="w-full"
                disabled={list.isFetchingNextPage}
                onClick={() => void list.fetchNextPage()}
              >
                {list.isFetchingNextPage ? (
                  <Loader2 data-icon="inline-start" className="animate-spin" />
                ) : null}
                <Trans>Show older</Trans>
              </Button>
            </div>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function NotificationRow({
  item,
  onOpen,
  onMarkRead,
}: {
  item: AppNotification;
  onOpen: () => void;
  onMarkRead: () => void;
}) {
  const { t } = useLingui();
  const f = useFormatters();
  const actor = item.actor.name;
  const doc = item.node.name;
  const unread = !item.read_at;

  return (
    <li className={cn("group/row relative", unread && "bg-accent/40")}>
      <button
        type="button"
        className="hover:bg-accent flex w-full cursor-pointer gap-2.5 px-3 py-2.5 pr-9 text-left text-xs"
        onClick={onOpen}
      >
        <SocialAvatar user={item.actor} />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="line-clamp-2">
            {item.kind === "mention" ? (
              <Trans>
                <strong className="font-medium">{actor}</strong> mentioned you in{" "}
                <strong className="font-medium">{doc}</strong>
              </Trans>
            ) : item.kind === "reply" ? (
              <Trans>
                <strong className="font-medium">{actor}</strong> replied in{" "}
                <strong className="font-medium">{doc}</strong>
              </Trans>
            ) : (
              <Trans>
                <strong className="font-medium">{actor}</strong> commented on{" "}
                <strong className="font-medium">{doc}</strong>
              </Trans>
            )}
          </span>
          {item.excerpt ? (
            <span className="text-muted-foreground line-clamp-2">{item.excerpt}</span>
          ) : null}
          <time
            dateTime={item.created_at}
            title={f.date(item.created_at)}
            className="text-muted-foreground text-[10px]"
          >
            {f.relative(item.created_at)}
          </time>
        </span>
      </button>
      {unread ? (
        <Button
          variant="ghost"
          size="icon-xs"
          className="absolute top-2.5 right-2"
          aria-label={t`Mark as read`}
          title={t`Mark as read`}
          onClick={onMarkRead}
        >
          <Circle className="fill-primary text-primary size-2" />
        </Button>
      ) : null}
    </li>
  );
}
