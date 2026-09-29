import { expect, test } from "vite-plus/test";
import {
  connectEventStream,
  reconnectDelay,
  type EventSourceLike,
} from "../src/lib/event-stream.ts";

test("reconnect delay doubles per attempt, stays in the ceiling's upper half, and caps", () => {
  const backoff = { base: 1_000, max: 30_000 };
  expect(reconnectDelay(0, backoff, () => 0)).toBe(500);
  expect(reconnectDelay(0, backoff, () => 1)).toBe(1_000);
  expect(reconnectDelay(3, backoff, () => 0)).toBe(4_000);
  expect(reconnectDelay(3, backoff, () => 1)).toBe(8_000);
  expect(reconnectDelay(10, backoff, () => 1)).toBe(30_000);
  expect(reconnectDelay(60, backoff, () => 0)).toBe(15_000);
});

class FakeSource implements EventSourceLike {
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;
  listeners = new Map<string, (event: MessageEvent<string>) => void>();
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    this.listeners.set(type, listener);
  }
  close() {
    this.closed = true;
  }
  open() {
    this.onopen?.(new Event("open"));
  }
  fail() {
    this.onerror?.(new Event("error"));
  }
  emit(type: string, data: string) {
    this.listeners.get(type)?.(new MessageEvent(type, { data }));
  }
}

interface Timer {
  fn: () => void;
  ms: number;
}

function harness(probe?: () => Promise<boolean>) {
  const sources: FakeSource[] = [];
  const probes: Promise<boolean>[] = [];
  const canReconnect = probe
    ? () => {
        const answer = probe();
        probes.push(answer);
        return answer;
      }
    : undefined;
  const timers: Timer[] = [];
  const cancelled = new Set<unknown>();
  const received: string[] = [];
  const opens: boolean[] = [];
  const stop = connectEventStream("/stream", {
    handlers: { notification: (data) => received.push(data) },
    onOpen: (reconnected) => opens.push(reconnected),
    canReconnect,
    backoff: { base: 100, max: 1_000 },
    random: () => 1,
    createSource: () => {
      const source = new FakeSource();
      sources.push(source);
      return source;
    },
    schedule: (fn, ms) => {
      const timer: Timer = { fn, ms };
      timers.push(timer);
      return timer;
    },
    cancel: (timer) => cancelled.add(timer),
  });
  const fire = () => timers.at(-1)!.fn();
  // Awaiting the probe queues behind the stream's own reaction to it.
  const settle = () =>
    probes.at(-1)!.then(
      () => undefined,
      () => undefined,
    );
  return { sources, timers, cancelled, received, opens, stop, fire, settle };
}

test("errors back off exponentially until an open resets the schedule", () => {
  const h = harness();
  h.sources[0]!.open();
  h.sources[0]!.emit("notification", "a");

  h.sources[0]!.fail();
  expect(h.sources[0]!.closed).toBe(true);
  h.fire();
  h.sources[1]!.fail();
  h.fire();
  h.sources[2]!.fail();
  expect(h.timers.map((t) => t.ms)).toEqual([100, 200, 400]);

  h.fire();
  h.sources[3]!.open();
  h.sources[3]!.fail();
  expect(h.timers.at(-1)!.ms).toBe(100);

  // The first open is not a reconnect; later ones are, so the caller refetches.
  expect(h.opens).toEqual([false, true]);
  expect(h.received).toEqual(["a"]);
});

test("a probe answering false stops for good; a failing probe backs off again", async () => {
  const answers = [() => Promise.reject(new Error("offline")), () => Promise.resolve(true)];
  const flaky = harness(() => answers.shift()?.() ?? Promise.resolve(false));
  flaky.sources[0]!.fail();
  flaky.fire();
  await flaky.settle();
  expect(flaky.sources).toHaveLength(1);
  expect(flaky.timers.map((t) => t.ms)).toEqual([100, 200]);
  flaky.fire();
  await flaky.settle();
  expect(flaky.sources).toHaveLength(2);

  const revoked = harness(() => Promise.resolve(false));
  revoked.sources[0]!.fail();
  revoked.fire();
  await revoked.settle();
  expect(revoked.sources).toHaveLength(1);
  expect(revoked.timers).toHaveLength(1);
});

test("stopping cancels a pending reconnect and ignores a late source", () => {
  const h = harness();
  h.sources[0]!.fail();
  h.stop();
  expect(h.cancelled.has(h.timers[0])).toBe(true);

  const h2 = harness();
  const live = h2.sources[0]!;
  h2.stop();
  expect(live.closed).toBe(true);
  live.emit("notification", "late");
  live.fail();
  expect(h2.received).toEqual([]);
  expect(h2.timers).toEqual([]);
});
