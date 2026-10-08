// What every extension's data layer needs and used to copy: the cache key
// shape, the retry policy, the "app is off" test and the live-event hook.

import { useEffect, useRef } from "react";
import type { QueryKey } from "@tanstack/react-query";
import { connectEventStream } from "./bridge";
import { ApiError } from "./host";

/** The cache key of an extension's query: `["ext", <ext id>, …parts]`. */
export function extQueryKey(extId: string, ...parts: readonly unknown[]) {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- cache key
  return ["ext", extId, ...parts] as const satisfies QueryKey;
}

/** The extension is switched off for this space or instance (the server answers 403). */
export function isAppOff(err: unknown): boolean {
  return err instanceof ApiError && err.status === 403;
}

/** Query retry policy: 4xx answers are answers, not hiccups; anything else gets two retries. */
export function extRetry(failureCount: number, err: unknown): boolean {
  const answered = err instanceof ApiError && err.status >= 400 && err.status < 500;
  return !answered && failureCount < 2;
}

/**
 * Wraps a handler of a stream event whose `data` is JSON. A malformed frame is
 * skipped; the next reconnect or `reload` heals the gap.
 */
export function jsonEvent<T>(handler: (event: T) => void): (data: string) => void {
  return (data) => {
    let event: T;
    try {
      event = JSON.parse(data) as T;
    } catch {
      return;
    }
    handler(event);
  };
}

export interface ExtEventsOptions {
  /** The stream's URL, `/api/v1/ext/<id>/…/events`. */
  url: string;
  /** Named events → handler of the event's `data` (see `jsonEvent`). */
  handlers: Record<string, (data: string) => void>;
  /**
   * Refetches the snapshot the stream patches, and rejects with the request's
   * error when it fails. Called before every reconnect, as a probe, and again
   * when a reconnect succeeds: events sent while the stream was down are not
   * replayed.
   */
  refetch: () => Promise<unknown>;
}

/**
 * Keeps an extension's data live while the component is mounted. `EventSource`
 * cannot say why a stream ended, so before reconnecting the snapshot is
 * refetched as a probe: a 401, 403 or 404 means the session, the app or the
 * node is gone, and the stream stops instead of retrying forever.
 */
export function useExtEvents({ url, handlers, refetch }: ExtEventsOptions): void {
  // The stream outlives renders; it reads the latest handlers and refetch
  // through this ref instead of reconnecting when they change identity.
  const latest = useRef({ handlers, refetch });
  useEffect(() => {
    latest.current = { handlers, refetch };
  });

  useEffect(
    () =>
      connectEventStream(url, {
        // The event names are read when the stream opens; each call site has a fixed set.
        handlers: Object.fromEntries(
          Object.keys(latest.current.handlers).map((name) => [
            name,
            (data: string) => latest.current.handlers[name]?.(data),
          ]),
        ),
        canReconnect: () =>
          latest.current.refetch().then(
            () => true,
            (err: unknown) => {
              if (err instanceof ApiError && [401, 403, 404].includes(err.status)) return false;
              throw err;
            },
          ),
        onOpen: (reconnected) => {
          if (reconnected) void latest.current.refetch().catch(() => undefined);
        },
      }),
    [url],
  );
}
