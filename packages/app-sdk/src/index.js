// @pp-hub/app-sdk — the frame side of the pp-hub packaged-app bridge (protocol 1).
// A plain ES module with no imports: load it with a bundler, or copy this file next to your
// page and `import { connect } from "./app-sdk.js"`. Types: index.d.ts beside this file.

/** The bridge protocol version this SDK speaks. */
export const PROTOCOL = 1;

/** A failed bridge call: `status` is HTTP-like (400, 403, 404, 409, 502, 503…). */
export class AppError extends Error {
  /**
   * @param {number} status
   * @param {string} message
   */
  constructor(status, message) {
    super(message);
    this.name = "AppError";
    this.status = status;
  }
}

const EVENTS = ["records", "broadcast", "theme", "stream"];

// The host offers its port on the frame's `load` event. A framework app may call `connect()`
// only after that (React runs effects after its first commit), so the offer is caught as soon
// as this module loads and kept for whichever `connect()` comes first.
/** @type {((e: MessageEvent) => void) | undefined} */
let onOffer;
/** @type {MessageEvent | undefined} */
let offer;
const win = globalThis.window;
const framed = !!win && !!win.parent && win.parent !== win;
if (framed) {
  /** @param {MessageEvent} e */
  const catchOffer = (e) => {
    if (e.source !== win.parent) return;
    const data = e.data;
    if (!data || typeof data !== "object" || data.type !== "pp-app:connect") return;
    if (!e.ports || !e.ports[0]) return;
    win.removeEventListener("message", catchOffer);
    offer = e;
    onOffer?.(e);
  };
  win.addEventListener("message", catchOffer);
}

/** @type {Promise<import("./index.js").App> | undefined} */
let connected;

/**
 * Waits for the host's `pp-app:connect` message, says hello and resolves the connected app.
 * The host connects a frame once: later calls return the first call's promise.
 * @param {{ applyTheme?: boolean, timeoutMs?: number }} [options]
 */
export function connect(options = {}) {
  if (!framed) {
    return Promise.reject(new AppError(0, "pp-app: not running inside a pp-hub frame"));
  }
  connected ??= open(options);
  return connected;
}

/** @param {{ applyTheme?: boolean, timeoutMs?: number }} options */
function open({ applyTheme = true, timeoutMs = 15000 }) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      onOffer = undefined;
      reject(new AppError(0, `pp-app: no connection from the host after ${timeoutMs} ms`));
    }, timeoutMs);
    /** @param {MessageEvent} e */
    const accept = (e) => {
      onOffer = undefined;
      clearTimeout(timer);
      if (e.data.protocol !== PROTOCOL) {
        reject(new AppError(0, `pp-app: host speaks protocol ${e.data.protocol}, SDK ${PROTOCOL}`));
        return;
      }
      const { app, call } = createApp(e.ports[0]);
      call("hello").then((context) => {
        app.context = context;
        if (applyTheme) {
          setTheme(context.theme);
          app.on("theme", setTheme);
        }
        resolve(app);
      }, reject);
    };
    if (offer) accept(offer);
    else onOffer = accept;
  });
}

/** @param {MessagePort} port */
function createApp(port) {
  let nextId = 1;
  /** @type {Map<number, { resolve: (v: unknown) => void, reject: (e: Error) => void }>} */
  const pending = new Map();
  /** @type {Map<string, Set<(data: unknown) => void>>} */
  const handlers = new Map(EVENTS.map((name) => [name, new Set()]));

  port.addEventListener("message", (e) => {
    const msg = e.data;
    if (!msg || typeof msg !== "object") return;
    if (typeof msg.event === "string") {
      for (const h of handlers.get(msg.event) ?? []) {
        try {
          h(msg.data);
        } catch (err) {
          console.error(err);
        }
      }
      return;
    }
    const p = pending.get(msg.id);
    if (!p) return;
    pending.delete(msg.id);
    if (msg.error) p.reject(new AppError(msg.error.status ?? 500, msg.error.message ?? "error"));
    else p.resolve(msg.result);
  });
  port.start();

  /**
   * @param {string} method
   * @param {object} [params]
   */
  function call(method, params) {
    return new Promise((resolve, reject) => {
      const id = nextId++;
      pending.set(id, { resolve, reject });
      port.postMessage(params === undefined ? { id, method } : { id, method, params });
    });
  }
  const none = () => undefined;

  const app = {
    /** @type {any} */
    context: null,
    records: {
      list: (collection) => call("records.list", collection === undefined ? {} : { collection }),
      get: (id) => call("records.get", { id }),
      create: (collection, data) => call("records.create", { collection, data }),
      update: (id, data, opts) => call("records.update", { id, data, if_rev: opts?.ifRev }),
      merge: (id, patch, opts) => call("records.update", { id, merge: patch, if_rev: opts?.ifRev }),
      delete: (id) => call("records.delete", { id }).then(none),
    },
    on(event, handler) {
      const set = handlers.get(event);
      if (!set) throw new TypeError(`pp-app: unknown event "${event}"`);
      set.add(handler);
      return () => void set.delete(handler);
    },
    broadcast: (event, data) => call("broadcast", { event, data }).then(none),
    people: { search: (query, limit) => call("people.search", { query, limit }) },
    users: { get: (ids) => call("users.get", { ids }) },
    links: { resolve: (ids) => call("links.resolve", { ids }) },
    nodes: {
      search: (query) => call("nodes.search", { query }),
      open: (id) => call("nodes.open", { id }).then(none),
    },
    comments: {
      open: (subject, thread) => call("comments.open", { subject, thread }).then(none),
      counts: () => call("comments.counts"),
    },
    notify: (n) => call("notify", n).then(none),
    ui: {
      toast: (message, variant) => call("ui.toast", { message, variant }).then(none),
      confirm: (o) =>
        call("ui.confirm", {
          title: o.title,
          message: o.message,
          confirm_label: o.confirmLabel,
          destructive: o.destructive,
        }),
    },
    backend: {
      fetch: (path, init = {}) =>
        call("backend.fetch", { path, ...init }).then((r) => ({
          ...r,
          ok: r.status >= 200 && r.status <= 299,
          json: () => JSON.parse(r.body),
        })),
    },
  };
  return { app, call };
}

/** @param {{ mode: "light" | "dark", vars: Record<string, string> } | undefined} theme */
function setTheme(theme) {
  const doc = globalThis.document;
  if (!theme || !doc) return;
  const root = doc.documentElement;
  for (const [name, value] of Object.entries(theme.vars ?? {})) root.style.setProperty(name, value);
  const dark = theme.mode === "dark";
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
}
