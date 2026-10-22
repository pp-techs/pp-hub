/* eslint-disable lingui/no-unlocalized-strings -- bridge protocol: method names, wire fields, URL paths and errors only an app's developer reads */
import { toast } from "sonner";
import { api, ApiError, capsOf, searchNodes, type Node, type UserProfile } from "@/lib/api";
import { connectEventStream } from "@/lib/event-stream";
import { openFile } from "@/lib/open-file";
import {
  BridgeError,
  object,
  PROTOCOL,
  serveBridge,
  str,
  uuid,
  uuids,
  type Bridge,
  type Method,
} from "./app-bridge";

// The page's side of a packaged app's frame (M51): what the app may ask for
// (contract §3), each scoped to the one node its frame was opened for — the
// server re-checks every one — and the wiring that hands the frame its port,
// its node's event stream and the theme.

/** How long a frame has, after its page loads, to say hello. */
const HELLO_TIMEOUT = 15_000;

/** The sandbox the frame runs in; the server's CSP sets the same one on every response. */
export const SANDBOX =
  "allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads";

/** The design tokens handed to the frame, so an app can look like pp-hub. */
const THEME_VARS = [
  "--background",
  "--foreground",
  "--card",
  "--card-foreground",
  "--popover",
  "--popover-foreground",
  "--primary",
  "--primary-foreground",
  "--secondary",
  "--secondary-foreground",
  "--muted",
  "--muted-foreground",
  "--accent",
  "--accent-foreground",
  "--destructive",
  "--border",
  "--input",
  "--ring",
  "--radius",
];

export interface AppTheme {
  mode: "light" | "dark";
  vars: Record<string, string>;
}

function currentTheme(): AppTheme {
  const root = document.documentElement;
  const style = getComputedStyle(root);
  return {
    mode: root.classList.contains("dark") ? "dark" : "light",
    vars: Object.fromEntries(THEME_VARS.map((v) => [v, style.getPropertyValue(v).trim()])),
  };
}

/** The response's JSON, or its error as the status and sentence the frame sees. */
async function read(res: Response): Promise<unknown> {
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.status === 204 ? null : res.json();
}

const BACKEND_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"];

/** What the methods read when they run: the latest of each. */
export interface HostState {
  node: Node | undefined;
  me: UserProfile | undefined;
  locale: string;
  subject?: string;
  thread?: string;
  /** The app's name in the UI's language. */
  name: string;
}

export interface ConfirmRequest {
  title: string;
  message: string;
  confirmLabel?: string;
  destructive: boolean;
}

/** The host's side effects the methods need from the page. */
export interface HostUI {
  state(): HostState;
  /** The frame said hello: it is running. */
  ready(): void;
  /** The frame did not say hello in time. */
  silent(): void;
  /** The frame navigated away from the app's page; its bridge is closed. */
  left(): void;
  openComments(subject: string, thread?: string): void;
  confirm(req: ConfirmRequest): Promise<boolean>;
}

/**
 * Asks the server whether this person may open the app on this item now —
 * the app is on for the space, the item is reachable — before any of the
 * app's code loads. Throws the server's ApiError (403: the app is off).
 */
export async function probeApp(appId: string, nodeId: string): Promise<true> {
  const res = await api.get(`api/v1/apps/${appId}/nodes/${nodeId}/records`, {
    searchParams: { limit: 1 },
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return true;
}

/**
 * Serves a frame for as long as it shows the app's page: on its first load,
 * a port (posted to "*" — the frame's origin is opaque, there is nothing to
 * address), the node's event stream and theme changes. A second load means
 * the frame navigated; a page that is not the app's own never gets a port,
 * so the bridge closes there. Returns the cleanup.
 */
export function attachFrame(
  frame: HTMLIFrameElement,
  app: { id: string; version: string },
  nodeId: string,
  ui: HostUI,
): () => void {
  const base = `api/v1/apps/${app.id}/nodes/${nodeId}`;
  let bridge: Bridge | null = null;
  let stopStream: (() => void) | undefined;
  let helloTimer: number | undefined;
  let themeObserver: MutationObserver | undefined;

  const methods = appMethods(app, nodeId, {
    ...ui,
    ready: () => {
      clearTimeout(helloTimer);
      ui.ready();
    },
  });

  const stop = () => {
    clearTimeout(helloTimer);
    themeObserver?.disconnect();
    stopStream?.();
    bridge?.close();
    bridge = null;
  };

  const onLoad = () => {
    if (bridge) {
      stop();
      ui.left();
      return;
    }
    const channel = new MessageChannel();
    const b = serveBridge(channel.port1, methods);
    bridge = b;
    frame.contentWindow?.postMessage({ type: "pp-app:connect", protocol: PROTOCOL }, "*", [
      channel.port2,
    ]);
    helloTimer = window.setTimeout(() => ui.silent(), HELLO_TIMEOUT);
    stopStream = connectEventStream(`/${base}/events`, {
      handlers: {
        app: (raw) => {
          const data = JSON.parse(raw) as {
            type: string;
            event?: string;
            data?: unknown;
            from?: string;
          };
          if (data.type === "broadcast") {
            b.emit("broadcast", { event: data.event, data: data.data, from: data.from });
          } else {
            b.emit("records", data);
          }
        },
      },
      onOpen: (reconnected) => b.emit("stream", { state: reconnected ? "reconnected" : "open" }),
      // A stream the server ended for lost access looks like any other
      // drop; ask before reconnecting.
      canReconnect: () =>
        api.get(`${base}/records`, { searchParams: { limit: 1 } }).then((res) => {
          if ([401, 403, 404].includes(res.status)) {
            b.emit("stream", { state: "closed" });
            return false;
          }
          return true;
        }),
    });
    let theme = JSON.stringify(currentTheme());
    themeObserver = new MutationObserver(() => {
      const next = currentTheme();
      const key = JSON.stringify(next);
      if (key === theme) return;
      theme = key;
      b.emit("theme", next);
    });
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style"],
    });
  };

  frame.addEventListener("load", onLoad);
  return () => {
    frame.removeEventListener("load", onLoad);
    stop();
  };
}

function appMethods(
  app: { id: string; version: string },
  nodeId: string,
  ui: HostUI,
): Record<string, Method> {
  const base = `api/v1/apps/${app.id}/nodes/${nodeId}`;
  return {
    async hello() {
      const { node: n, me: u, locale, subject, thread } = ui.state();
      if (!n || !u) throw new BridgeError(503, "pp-hub is still loading");
      ui.ready();
      return {
        protocol: PROTOCOL,
        app: { id: app.id, version: app.version },
        node: { id: n.id, name: n.name, space_id: n.space_id, ext_kind: n.ext_kind ?? "" },
        user: { id: u.id, name: u.display_name || u.name || u.email, email: u.email },
        caps: capsOf(n),
        locale,
        theme: currentTheme(),
        subject,
        thread,
      };
    },
    async "records.list"(p) {
      const collection = str(p, "collection", { optional: true, max: 32 });
      const out: unknown[] = [];
      let after = "";
      // The host pages through everything; an item holds at most 10000.
      for (;;) {
        const page = (await read(
          await api.get(`${base}/records`, {
            searchParams: { collection: collection || undefined, after: after || undefined },
          }),
        )) as { records: unknown[]; next: string };
        out.push(...page.records);
        if (!page.next) return out;
        after = page.next;
      }
    },
    async "records.get"(p) {
      return read(await api.get(`${base}/records/${uuid(p, "id")}`));
    },
    async "records.create"(p) {
      return read(
        await api.post(`${base}/records`, {
          json: { collection: str(p, "collection", { max: 32 }), data: object(p, "data") },
        }),
      );
    },
    async "records.update"(p) {
      const ifRev = p.if_rev;
      if (ifRev !== undefined && ifRev !== null && typeof ifRev !== "number") {
        throw new BridgeError(400, "if_rev must be a number");
      }
      return read(
        await api.patch(`${base}/records/${uuid(p, "id")}`, {
          json: {
            data: object(p, "data", true),
            merge: object(p, "merge", true),
            if_rev: ifRev ?? undefined,
          },
        }),
      );
    },
    async "records.delete"(p) {
      return read(await api.delete(`${base}/records/${uuid(p, "id")}`));
    },
    async broadcast(p) {
      return read(
        await api.post(`${base}/broadcast`, {
          json: { event: str(p, "event", { max: 64 }), data: p.data ?? null },
        }),
      );
    },
    async "people.search"(p) {
      const limit = typeof p.limit === "number" ? p.limit : undefined;
      return read(
        await api.get(`${base}/people`, {
          searchParams: { q: str(p, "query", { max: 200 }), limit },
        }),
      );
    },
    async "users.get"(p) {
      const ids = uuids(p, "ids");
      if (ids.length === 0) return [];
      return read(await api.get(`${base}/users`, { searchParams: { ids: ids.join(",") } }));
    },
    async "links.resolve"(p) {
      const ids = uuids(p, "ids");
      if (ids.length === 0) return [];
      return read(await api.get(`${base}/links`, { searchParams: { ids: ids.join(",") } }));
    },
    async "nodes.search"(p) {
      const hits = await searchNodes(str(p, "query", { max: 200 }), {
        spaceId: ui.state().node?.space_id,
        limit: 20,
      });
      return hits.map((h) => ({
        id: h.id,
        space_id: h.space_id,
        name: h.name,
        kind: h.kind,
        ext_kind: h.ext_kind,
        parent_name: h.parent_name,
      }));
    },
    async "nodes.open"(p) {
      openFile({ id: uuid(p, "id") });
      return null;
    },
    async "comments.open"(p) {
      const subject = str(p, "subject", { max: 100 });
      const thread = str(p, "thread", { optional: true, max: 64 });
      ui.openComments(subject, thread || undefined);
      return null;
    },
    async "comments.counts"() {
      return read(await api.get(`${base}/comments/counts`));
    },
    async notify(p) {
      return read(
        await api.post(`${base}/notify`, {
          json: {
            to: uuid(p, "to"),
            verb: str(p, "verb", { max: 32 }),
            excerpt: str(p, "excerpt", { optional: true, max: 500 }),
            subject: str(p, "subject", { optional: true, max: 100 }),
          },
        }),
      );
    },
    async "ui.toast"(p) {
      const message = str(p, "message", { max: 300 });
      // The app's name rides along, so a frame cannot pass its words off as
      // pp-hub's own.
      const opts = { description: ui.state().name };
      if (p.variant === "success") toast.success(message, opts);
      else if (p.variant === "error") toast.error(message, opts);
      else toast(message, opts);
      return null;
    },
    async "ui.confirm"(p) {
      return ui.confirm({
        title: str(p, "title", { max: 200 }),
        message: str(p, "message", { max: 1000 }),
        confirmLabel: str(p, "confirm_label", { optional: true, max: 40 }) || undefined,
        destructive: p.destructive === true,
      });
    },
    async "backend.fetch"(p) {
      const path = str(p, "path", { max: 2000 }).replace(/^\/+/, "");
      const method = (str(p, "method", { optional: true }) || "GET").toUpperCase();
      if (!BACKEND_METHODS.includes(method)) {
        throw new BridgeError(400, `method must be one of ${BACKEND_METHODS.join(", ")}`);
      }
      const headers: Record<string, string> = {};
      for (const [k, v] of Object.entries(object(p, "headers", true) ?? {})) {
        if (typeof v === "string") headers[k] = v;
      }
      const body = str(p, "body", { optional: true, max: 1 << 20 });
      const res = await api(`${base}/backend/${path}`, {
        method,
        headers,
        body: body && method !== "GET" ? body : undefined,
      });
      return {
        status: res.status,
        headers: Object.fromEntries(res.headers.entries()),
        body: await res.text(),
      };
    },
  };
}
