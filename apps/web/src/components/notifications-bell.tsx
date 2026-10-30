import { useState } from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Bell, CheckCheck, Circle, Loader2, Puzzle } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Popover, PopoverContent, PopoverTrigger } from "@workspace/ui/components/popover";
import { Skeleton } from "@workspace/ui/components/skeleton";
import { cn } from "@workspace/ui/lib/utils";
import type { AppNotification } from "@/lib/api";
import { openFile } from "@/lib/open-file";
import { extensionById, useExtRegistry } from "@/ext/registry";
import { extNotificationIcon, extNotificationMessage } from "@/lib/ext-notification";
import { useFormatters } from "@/lib/format";
import {
  useMarkNotificationsRead,
  useNotificationStream,
  useNotifications,
  useUnreadNotificationCount,
} from "@/lib/queries";
import { LoadingStatus } from "@/components/loading";
import { SocialAvatar } from "@/components/comments/social-avatar";

/**
 * The inbox: a bell with the unread count, opening the newest notifications.
 * Mounted once, in the drive header, and it holds the page's live stream so
 * the badge moves without a refresh.
 *
 * Opening one marks it read and goes to its node through `/open/$nodeId`,
 * which knows what each kind opens in — a file in the shell, on its subject
 * and with its thread focused; a shared folder in its listing.
 */
export function NotificationsBell() {
  const { t } = useLingui();
  const [open, setOpen] = useState(false);
  useNotificationStream();
  const unread = useUnreadNotificationCount();
  const list = useNotifications(open);
  const markRead = useMarkNotificationsRead();

  const count = unread.data ?? 0;
  const items = list.data?.pages.flatMap((page) => page.items) ?? [];

  const openItem = (item: AppNotification) => {
    if (!item.read_at) markRead.mutate([item.id]);
    setOpen(false);
    openFile({ id: item.node.id }, { thread: item.thread_id, subject: item.subject });
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
      <PopoverContent align="end" className="w-96 max-w-[calc(100vw-1rem)] gap-0 p-0">
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

/** The manifest's line with the actor's name, when it opens the sentence, in the medium weight of the core lines. */
function ExtLine({ text, actor }: { text: string; actor: string }) {
  if (!actor || !text.startsWith(actor)) return text;
  return (
    <>
      <strong className="font-medium">{actor}</strong>
      {text.slice(actor.length)}
    </>
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
  const { t, i18n } = useLingui();
  const f = useFormatters();
  const actor = item.actor.name;
  const doc = item.node.name;
  useExtRegistry();
  const extLine = extNotificationMessage(item, extensionById);
  const unread = !item.read_at;
  const ExtIcon = extNotificationIcon(item.kind, extensionById) ?? Puzzle;

  return (
    <li className={cn("group/row relative", unread && "bg-accent/40")}>
      <button
        type="button"
        className="hover:bg-accent flex w-full cursor-pointer gap-2.5 px-3 py-2.5 pr-9 text-left text-xs"
        onClick={onOpen}
      >
        <SocialAvatar user={item.actor} />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="line-clamp-2 leading-snug">
            {extLine ? (
              <ExtLine text={i18n._(extLine)} actor={actor} />
            ) : item.kind === "mention" ? (
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
          {extLine ? (
            <span className="bg-muted text-muted-foreground flex max-w-full items-center gap-1 self-start rounded px-1.5 py-0.5 text-[10px]">
              <ExtIcon className="size-3 shrink-0" />
              <span className="truncate">{doc}</span>
            </span>
          ) : item.excerpt ? (
            <span className="bg-muted/60 text-muted-foreground border-primary line-clamp-2 rounded border-l-2 px-2 py-1 italic">
              {item.excerpt}
            </span>
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
