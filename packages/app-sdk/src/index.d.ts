// Types of @pp-hub/app-sdk (index.js beside this file): the frame side of the pp-hub
// packaged-app bridge, protocol 1.

/** The bridge protocol version this SDK speaks. */
export declare const PROTOCOL: 1;

/** A failed bridge call. */
export declare class AppError extends Error {
  /** HTTP-like status: 400 bad params, 403 no permission or app off, 404, 409 rev conflict, 502/503 backend; 0 when the bridge itself failed. */
  status: number;
  constructor(status: number, message: string);
}

/** What the host tells the app about where it runs; `App.context`. */
export interface Context {
  /** Always 1. */
  protocol: 1;
  /** The app as installed. */
  app: { id: string; version: string };
  /** The node (document) this frame shows. */
  node: { id: string; name: string; space_id: string; ext_kind: string };
  /** The signed-in viewer. */
  user: { id: string; name: string; email: string };
  /** What the viewer may do with the node; write calls fail with 403 without `edit`. */
  caps: Caps;
  /** BCP 47 tag of the host UI language, e.g. "en", "vi". */
  locale: string;
  /** The host theme; applied automatically unless `connect({ applyTheme: false })`. */
  theme: Theme;
  /** A subject to focus, e.g. "record:<uuid>" from a notification link. */
  subject?: string;
  /** A comment thread id to focus. */
  thread?: string;
}

/** The viewer's capabilities on the node. */
export interface Caps {
  view: boolean;
  download: boolean;
  share: boolean;
  edit: boolean;
  owner: boolean;
}

/** The host theme. */
export interface Theme {
  mode: "light" | "dark";
  /** CSS custom properties, keys like "--background". */
  vars: Record<string, string>;
}

/** A record in the host-provided store, scoped to the node. */
export interface AppRecord {
  id: string;
  /** Collection name, ^[a-z][a-z0-9_]{0,31}$. */
  collection: string;
  /** The app's own JSON object. */
  data: Record<string, unknown>;
  /** Incremented on every write; pass as `ifRev` for optimistic concurrency. */
  rev: number;
  created_by: string | null;
  updated_by: string | null;
  /** RFC 3339 timestamp. */
  created_at: string;
  /** RFC 3339 timestamp. */
  updated_at: string;
}

/** A person who can open the node. */
export interface Person {
  id: string;
  name: string;
  email?: string;
}

/** A node the viewer can open. */
export interface LinkTarget {
  id: string;
  space_id: string;
  name: string;
  kind: "dir" | "file" | "ext";
  ext_kind?: string;
}

/** A name-search hit in the node's space. */
export interface NodeHit extends LinkTarget {
  parent_name: string;
}

/** Payload of the `records` event. */
export type RecordsEvent =
  | { type: "record.upsert"; record: AppRecord }
  | { type: "record.delete"; id: string; collection: string };

/** Payload of the `broadcast` event; `from` is the sender's user id. */
export interface BroadcastEvent {
  event: string;
  data: unknown;
  from: string;
}

/** Payload of the `stream` event; on "reconnected" an app refetches what it shows. */
export interface StreamEvent {
  state: "open" | "reconnected" | "closed";
}

/** Event name → payload type of `App.on`. */
export interface AppEvents {
  records: RecordsEvent;
  broadcast: BroadcastEvent;
  theme: Theme;
  stream: StreamEvent;
}

/** A notification to send; `verb` names a template in the manifest's `notifications`. */
export interface Notification {
  /** Recipient user id. */
  to: string;
  verb: string;
  /** Short text substituted for `{excerpt}`. */
  excerpt?: string;
  /** What the notification links to, e.g. "record:<uuid>"; arrives as `context.subject`. */
  subject?: string;
}

/** Request to the app's own backend, proxied by the host with a signed token. */
export interface BackendRequest {
  method?: string;
  headers?: Record<string, string>;
  body?: string;
}

/** The backend's response. */
export interface BackendResponse {
  status: number;
  /** True for 200–299. */
  ok: boolean;
  headers: Record<string, string>;
  body: string;
  /** `JSON.parse(body)`. */
  json<T = unknown>(): T;
}

/** The connected app. */
export interface App {
  /** The `hello` result. */
  context: Context;
  /** The node's record store. */
  records: {
    /** Every record, or those of one collection, ordered by created_at then id. */
    list(collection?: string): Promise<AppRecord[]>;
    /** One record; 404 when gone. */
    get(id: string): Promise<AppRecord>;
    /** Creates a record. */
    create(collection: string, data: object): Promise<AppRecord>;
    /** Replaces a record's data; with `ifRev`, fails with 409 when the record changed meanwhile. */
    update(id: string, data: object, opts?: { ifRev?: number }): Promise<AppRecord>;
    /** Shallow-merges `patch` into a record's data; with `ifRev`, fails with 409 when it changed. */
    merge(id: string, patch: object, opts?: { ifRev?: number }): Promise<AppRecord>;
    /** Deletes a record. */
    delete(id: string): Promise<void>;
  };
  /** Subscribes to a host event; returns the unsubscribe function. */
  on<E extends keyof AppEvents>(event: E, handler: (data: AppEvents[E]) => void): () => void;
  /** Sends an ephemeral event to every other viewer of the node (not stored). */
  broadcast(event: string, data?: unknown): Promise<void>;
  /** People who can open the node. */
  people: { search(query: string, limit?: number): Promise<Person[]> };
  /** Users by id (unknown ids are left out). */
  users: { get(ids: string[]): Promise<Person[]> };
  /** Nodes by id, only those the viewer can open. */
  links: { resolve(ids: string[]): Promise<LinkTarget[]> };
  /** Nodes in the node's space. */
  nodes: {
    /** Name search. */
    search(query: string): Promise<NodeHit[]>;
    /** Asks the host to navigate to a node. */
    open(id: string): Promise<void>;
  };
  /** The host's comments, attached to subjects like "record:<uuid>". */
  comments: {
    /** Opens the host comment panel on a subject (and thread). */
    open(subject: string, thread?: string): Promise<void>;
    /** Comment count per subject. */
    counts(): Promise<Record<string, number>>;
  };
  /** Notifies a person who can open the node. */
  notify(n: Notification): Promise<void>;
  /** Host UI. */
  ui: {
    /** Shows a host toast. */
    toast(message: string, variant?: "default" | "success" | "error"): Promise<void>;
    /** Shows a host confirm dialog; resolves true when confirmed. */
    confirm(o: {
      title: string;
      message: string;
      confirmLabel?: string;
      destructive?: boolean;
    }): Promise<boolean>;
  };
  /** The app's own backend (`backend.url` in the manifest). */
  backend: {
    /** Requests `<backend.url>/<path>` through the host. */
    fetch(path: string, init?: BackendRequest): Promise<BackendResponse>;
  };
}

/** Options of `connect`. */
export interface ConnectOptions {
  /** Write the host theme onto `document.documentElement` and follow `theme` events. Default true. */
  applyTheme?: boolean;
  /** How long to wait for the host handshake, in ms. Default 15000. */
  timeoutMs?: number;
}

/**
 * Waits for the host's handshake, calls `hello` and resolves the connected app. The handshake is
 * caught when this module loads, so calling it late (from a framework effect) is fine; every call
 * returns the first call's promise.
 */
export declare function connect(options?: ConnectOptions): Promise<App>;
