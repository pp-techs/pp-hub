import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { PROTOCOL, type Context } from "../src/index.js";

type Listener = (e: { source: unknown; data: unknown; ports: MessagePort[] }) => void;

const context: Context = {
  protocol: 1,
  app: { id: "tasks", version: "1.0.0" },
  node: { id: "n1", name: "List", space_id: "s1", ext_kind: "tasks.list" },
  user: { id: "u1", name: "Ann", email: "ann@example.com" },
  caps: { view: true, download: true, share: false, edit: true, owner: false },
  locale: "en",
  theme: { mode: "dark", vars: { "--background": "#000", "--foreground": "#fff" } },
};

/**
 * A fake `window` with a distinct parent, a fake `document`, a host on the other end of a
 * MessageChannel, and a fresh copy of the SDK loaded inside that window.
 */
async function setup() {
  const listeners = new Set<Listener>();
  const parent = {};
  const props = new Map<string, string>();
  const classes = new Set<string>();
  const style = {
    colorScheme: "",
    setProperty: (k: string, v: string) => void props.set(k, v),
  };
  const g = globalThis as Record<string, unknown>;
  g.window = {
    parent,
    addEventListener: (_: string, l: Listener) => void listeners.add(l),
    removeEventListener: (_: string, l: Listener) => void listeners.delete(l),
  };
  g.document = {
    documentElement: {
      style,
      classList: {
        toggle: (c: string, on: boolean) => void (on ? classes.add(c) : classes.delete(c)),
      },
    },
  };
  const channel = new MessageChannel();
  const host = channel.port1;
  const requests: { id: number; method: string; params?: Record<string, unknown> }[] = [];
  const answers: Record<string, (params: Record<string, unknown> | undefined) => unknown> = {
    hello: () => context,
  };
  host.addEventListener("message", (e: MessageEvent) => {
    const req = e.data;
    requests.push(req);
    const answer = answers[req.method];
    if (!answer) {
      host.postMessage({ id: req.id, error: { status: 404, message: `no ${req.method}` } });
      return;
    }
    host.postMessage({ id: req.id, result: answer(req.params) });
  });
  host.start();
  const dispatch = (source: unknown, data: unknown, ports: MessagePort[] = []) => {
    for (const l of listeners) l({ source, data, ports });
  };
  const handshake = () =>
    dispatch(parent, { type: "pp-app:connect", protocol: PROTOCOL }, [channel.port2]);
  cleanups.push(() => {
    host.close();
    channel.port2.close();
    delete g.window;
    delete g.document;
  });
  vi.resetModules();
  const { AppError, connect } = await import("../src/index.js");
  return {
    AppError,
    connect,
    host,
    requests,
    answers,
    dispatch,
    handshake,
    parent,
    listeners,
    props,
    classes,
    style,
  };
}

const cleanups: (() => void)[] = [];
afterEach(() => {
  for (const c of cleanups.splice(0)) c();
});

describe("connect", () => {
  it("handshakes, says hello and applies the theme", async () => {
    const t = await setup();
    const pending = t.connect();
    t.handshake();
    const app = await pending;
    expect(app.context).toEqual(context);
    expect(t.requests[0]).toEqual({ id: 1, method: "hello" });
    expect(t.props.get("--background")).toBe("#000");
    expect(t.classes.has("dark")).toBe(true);
    expect(t.style.colorScheme).toBe("dark");
    expect(t.listeners.size).toBe(0);

    // The SDK's own theme handler was registered first, so it has run when this one fires.
    const { promise: themed, resolve } = Promise.withResolvers<unknown>();
    app.on("theme", resolve);
    t.host.postMessage({
      event: "theme",
      data: { mode: "light", vars: { "--background": "#fff" } },
    });
    await themed;
    expect(t.props.get("--background")).toBe("#fff");
    expect(t.classes.has("dark")).toBe(false);
    expect(t.style.colorScheme).toBe("light");
  });

  it("keeps a handshake that arrives before connect() is called", async () => {
    const t = await setup();
    t.handshake();
    const app = await t.connect();
    expect(app.context).toEqual(context);
    expect(await t.connect()).toBe(app);
  });

  it("ignores messages from anyone but the parent", async () => {
    const t = await setup();
    const other = new MessageChannel();
    cleanups.push(() => other.port2.close());
    const pending = t.connect({ timeoutMs: 200 });
    t.dispatch({}, { type: "pp-app:connect", protocol: 1 }, [other.port2]);
    t.dispatch(t.parent, { type: "something-else" }, []);
    t.handshake();
    await expect(pending).resolves.toBeTruthy();
  });

  it("times out without a handshake", async () => {
    const t = await setup();
    vi.useFakeTimers();
    cleanups.push(() => vi.useRealTimers());
    const pending = t.connect({ timeoutMs: 1000 });
    vi.advanceTimersByTime(1000);
    await expect(pending).rejects.toBeInstanceOf(t.AppError);
  });

  it("skips the theme with applyTheme: false", async () => {
    const t = await setup();
    const pending = t.connect({ applyTheme: false });
    t.handshake();
    await pending;
    expect(t.props.size).toBe(0);
  });

  it("maps every method to the bridge request of the contract", async () => {
    const t = await setup();
    const echo = (p: unknown) => p ?? null;
    for (const m of [
      "records.list",
      "records.get",
      "records.create",
      "records.update",
      "records.delete",
      "broadcast",
      "people.search",
      "users.get",
      "links.resolve",
      "nodes.search",
      "nodes.open",
      "comments.open",
      "notify",
      "ui.toast",
      "ui.confirm",
    ])
      t.answers[m] = echo;
    t.answers["comments.counts"] = () => ({ "record:1": 2 });
    t.answers["backend.fetch"] = () => ({ status: 201, headers: {}, body: '{"a":1}' });
    const pending = t.connect();
    t.handshake();
    const app = await pending;

    expect(await app.records.list("items")).toEqual({ collection: "items" });
    expect(await app.records.list()).toEqual({});
    expect(await app.records.get("r1")).toEqual({ id: "r1" });
    expect(await app.records.create("items", { a: 1 })).toEqual({
      collection: "items",
      data: { a: 1 },
    });
    expect(await app.records.update("r1", { a: 2 }, { ifRev: 3 })).toEqual({
      id: "r1",
      data: { a: 2 },
      if_rev: 3,
    });
    expect(await app.records.merge("r1", { b: 1 })).toEqual({ id: "r1", merge: { b: 1 } });
    expect(await app.records.delete("r1")).toBeUndefined();
    expect(await app.broadcast("ping", 1)).toBeUndefined();
    expect(await app.people.search("an", 5)).toEqual({ query: "an", limit: 5 });
    expect(await app.users.get(["u1"])).toEqual({ ids: ["u1"] });
    expect(await app.links.resolve(["n1"])).toEqual({ ids: ["n1"] });
    expect(await app.nodes.search("x")).toEqual({ query: "x" });
    await app.nodes.open("n2");
    await app.comments.open("record:1", "t1");
    expect(await app.comments.counts()).toEqual({ "record:1": 2 });
    await app.notify({ to: "u2", verb: "assigned", excerpt: "x", subject: "record:1" });
    await app.ui.toast("hi", "success");
    expect(
      await app.ui.confirm({ title: "T", message: "M", confirmLabel: "Go", destructive: true }),
    ).toEqual({ title: "T", message: "M", confirm_label: "Go", destructive: true });
    const res = await app.backend.fetch("/x", { method: "POST", body: "{}" });
    expect(res.ok).toBe(true);
    expect(res.json()).toEqual({ a: 1 });

    const sent = t.requests.map((r) => [r.method, r.params]);
    expect(sent).toContainEqual(["records.update", { id: "r1", merge: { b: 1 } }]);
    expect(sent).toContainEqual(["nodes.open", { id: "n2" }]);
    expect(sent).toContainEqual(["comments.open", { subject: "record:1", thread: "t1" }]);
    expect(sent).toContainEqual([
      "notify",
      { to: "u2", verb: "assigned", excerpt: "x", subject: "record:1" },
    ]);
    expect(sent).toContainEqual(["ui.toast", { message: "hi", variant: "success" }]);
    expect(sent).toContainEqual(["backend.fetch", { path: "/x", method: "POST", body: "{}" }]);
    expect(sent).toContainEqual(["comments.counts", undefined]);
    const ids = t.requests.map((r) => r.id);
    expect(ids).toEqual(ids.map((_, i) => i + 1));
  });

  it("rejects with AppError carrying the status", async () => {
    const t = await setup();
    const pending = t.connect();
    t.handshake();
    const app = await pending;
    const err = await app.records.get("nope").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(t.AppError);
    expect((err as InstanceType<typeof t.AppError>).status).toBe(404);
  });

  it("delivers events until unsubscribed", async () => {
    const t = await setup();
    const pending = t.connect();
    t.handshake();
    const app = await pending;
    const got: unknown[] = [];
    const off = app.on("records", (d) => got.push(d));
    const ev = { type: "record.delete", id: "r1", collection: "items" };
    t.host.postMessage({ event: "records", data: ev });
    // A round trip: its response follows the event on the same ordered port.
    await app.records.get("sync").catch(() => {});
    off();
    t.host.postMessage({ event: "records", data: ev });
    await app.records.get("sync").catch(() => {});
    expect(got).toEqual([ev]);
    expect(() => app.on("nope" as "records", () => {})).toThrow(/unknown event/);
  });
});
