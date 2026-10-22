import { Trans } from "@lingui/react/macro";
import { CircleAlert, PowerOff } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@workspace/ui/components/empty";
import { ApiError } from "@/lib/api";

/** Whether a failed request says the app is switched off for the space. */
export function isAppOff(err: unknown): boolean {
  return err instanceof ApiError && err.status === 403;
}

/** Query retry policy: 4xx answers are answers, not hiccups; anything else gets two retries. */
export function appRetry(failureCount: number, err: unknown): boolean {
  const answered = err instanceof ApiError && err.status >= 400 && err.status < 500;
  return !answered && failureCount < 2;
}

/**
 * The state of an app's editor that could not open its item: the app is
 * switched off for the space (403), or the item is gone or out of reach.
 * `what` is the item's name for itself, already translated and lower-case
 * ("kanban board"); `onRetry` asks again, offered unless the app is off.
 */
export function ExtUnavailable({
  what,
  off,
  onRetry,
}: {
  what: string;
  off: boolean;
  onRetry: () => void;
}) {
  return (
    <Empty className="h-full">
      <EmptyHeader>
        <EmptyMedia variant="icon">{off ? <PowerOff /> : <CircleAlert />}</EmptyMedia>
        <EmptyTitle>
          {off ? (
            <Trans>This app is turned off</Trans>
          ) : (
            <Trans>This {what} could not be opened</Trans>
          )}
        </EmptyTitle>
        <EmptyDescription>
          {off ? (
            <Trans>
              This app is switched off for this space. Your {what} is kept; an admin can turn the
              app back on.
            </Trans>
          ) : (
            <Trans>It may have been deleted, or you may no longer have access to it.</Trans>
          )}
        </EmptyDescription>
      </EmptyHeader>
      {!off && (
        <Button variant="outline" onClick={onRetry}>
          <Trans>Try again</Trans>
        </Button>
      )}
    </Empty>
  );
}
