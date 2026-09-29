/**
 * A Server-Sent Events subscription that survives the server going away.
 *
 * The browser's own `EventSource` retry is not enough: it retries a dropped
 * connection at a fixed interval, and gives up for good on any non-200
 * answer (a restart behind nginx answers 502 for a moment). So every error
 * closes the source and reconnects on our own schedule — exponential with
 * jitter, capped — and a successful open resets it.
 */

export interface ReconnectBackoff {
  /** First delay, in milliseconds. */
  base: number;
  /** No delay grows past this. */
  max: number;
}

export const DEFAULT_BACKOFF: ReconnectBackoff = { base: 1_000, max: 30_000 };

/**
 * How long to wait before reconnect number `attempt` (0-based). The ceiling
 * doubles per attempt up to `max`; the wait is drawn from its upper half, so
 * a fleet of tabs dropped by one restart does not reconnect in lockstep yet
 * never retries sooner than half the ceiling.
 */
export function reconnectDelay(
  attempt: number,
  backoff: ReconnectBackoff = DEFAULT_BACKOFF,
  random: () => number = Math.random,
): number {
  const ceiling = Math.min(backoff.max, backoff.base * 2 ** Math.max(0, attempt));
  return Math.round(ceiling / 2 + (random() * ceiling) / 2);
}

/** The part of `EventSource` this module uses, so tests can drive a fake one. */
export interface EventSourceLike {
  onopen: ((event: Event) => void) | null;
  onerror: ((event: Event) => void) | null;
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
}

export interface EventStreamOptions {
  /** Named events → handler of the event's `data`. */
  handlers: Record<string, (data: string) => void>;
  /**
   * Called on every successful open. `reconnected` is true from the second
   * one on: events sent while the stream was down are gone, so the caller
   * refetches whatever they would have updated.
   */
  onOpen?: (reconnected: boolean) => void;
  /**
   * Asked before each reconnect. `EventSource` cannot see an HTTP status, so
   * a stream the server ended for a revoked session looks like any other
   * drop; resolving `false` stops for good, rejecting counts as one more
   * failed attempt. Without it every drop reconnects.
   */
  canReconnect?: () => Promise<boolean>;
  backoff?: ReconnectBackoff;
  createSource?: (url: string) => EventSourceLike;
  schedule?: (fn: () => void, ms: number) => unknown;
  cancel?: (handle: unknown) => void;
  random?: () => number;
}

/** Opens the stream and keeps it open until the returned function is called. */
export function connectEventStream(url: string, options: EventStreamOptions): () => void {
  const {
    handlers,
    onOpen,
    canReconnect,
    backoff = DEFAULT_BACKOFF,
    createSource = (u) => new EventSource(u, { withCredentials: true }),
    schedule = (fn, ms) => setTimeout(fn, ms),
    cancel = (handle) => clearTimeout(handle as number),
    random = Math.random,
  } = options;

  let source: EventSourceLike | null = null;
  let timer: unknown = null;
  let attempt = 0;
  let opened = false;
  let stopped = false;

  const reconnect = () => {
    timer = schedule(
      () => {
        timer = null;
        if (stopped) return;
        if (!canReconnect) {
          connect();
          return;
        }
        canReconnect().then(
          (ok) => {
            if (ok && !stopped) connect();
          },
          () => {
            if (!stopped) reconnect();
          },
        );
      },
      reconnectDelay(attempt, backoff, random),
    );
    attempt += 1;
  };

  const connect = () => {
    const current = createSource(url);
    source = current;
    current.onopen = () => {
      if (stopped || source !== current) return;
      attempt = 0;
      onOpen?.(opened);
      opened = true;
    };
    current.onerror = () => {
      if (stopped || source !== current) return;
      current.close();
      source = null;
      reconnect();
    };
    for (const [type, handler] of Object.entries(handlers)) {
      current.addEventListener(type, (event) => {
        if (!stopped && source === current) handler(event.data);
      });
    }
  };

  connect();

  return () => {
    stopped = true;
    if (timer !== null) cancel(timer);
    source?.close();
    source = null;
  };
}
