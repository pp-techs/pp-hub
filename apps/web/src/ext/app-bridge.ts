/* eslint-disable lingui/no-unlocalized-strings -- bridge protocol: wire fields and errors only an app's developer reads */
// The host half of the packaged-app bridge (M51, protocol 1; the frame half is
// `@pp-hub/app-sdk`). A packaged app's frame has an opaque origin and no
// session, so it cannot call pp-hub itself: it sends requests over a
// MessagePort and the page that hosts it answers them, scoped to the one node
// the frame was opened for. Nothing here trusts the frame: every request is a
// method on a fixed list, every parameter is checked, and the server checks
// access again behind it.

export const PROTOCOL = 1;

/** A request that failed in a way the frame should see as a status. */
export class BridgeError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "BridgeError";
    this.status = status;
  }
}

export type Params = Record<string, unknown>;
export type Method = (params: Params) => Promise<unknown>;

export interface Bridge {
  /** An event to the frame: `records`, `broadcast`, `theme`, `stream`. */
  emit(event: string, data: unknown): void;
  close(): void;
}

function statusOf(err: unknown): number {
  if (err && typeof err === "object" && "status" in err && typeof err.status === "number") {
    return err.status;
  }
  return 500;
}

/**
 * Answers the frame's requests on port with methods. A malformed message is
 * dropped; an unknown method is a 404; a throw is an error response carrying
 * the thrower's status (an `ApiError`'s or a `BridgeError`'s), else 500.
 */
export function serveBridge(port: MessagePort, methods: Record<string, Method>): Bridge {
  let open = true;
  port.onmessage = (event: MessageEvent) => {
    const msg: unknown = event.data;
    if (!msg || typeof msg !== "object") return;
    const { id, method, params } = msg as { id?: unknown; method?: unknown; params?: unknown };
    if (typeof id !== "number" || typeof method !== "string") return;
    const reply = (body: object) => {
      if (open) port.postMessage({ id, ...body });
    };
    const fn = Object.hasOwn(methods, method) ? methods[method] : undefined;
    if (!fn) {
      reply({ error: { status: 404, message: `unknown method ${method}` } });
      return;
    }
    const p =
      params && typeof params === "object" && !Array.isArray(params) ? (params as Params) : {};
    fn(p).then(
      (result) => reply({ result: result ?? null }),
      (err: unknown) =>
        reply({
          error: {
            status: statusOf(err),
            message: err instanceof Error ? err.message : String(err),
          },
        }),
    );
  };
  port.start();
  return {
    emit(event, data) {
      if (open) port.postMessage({ event, data });
    },
    close() {
      open = false;
      port.onmessage = null;
      port.close();
    },
  };
}

// Parameter readers: each throws a 400 the frame can read.

export function str(
  p: Params,
  key: string,
  opts: { optional?: boolean; max?: number } = {},
): string {
  const v = p[key];
  if (v === undefined && opts.optional) return "";
  if (typeof v !== "string") throw new BridgeError(400, `${key} must be a string`);
  if (opts.max !== undefined && v.length > opts.max) {
    throw new BridgeError(400, `${key} is longer than ${opts.max} characters`);
  }
  return v;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function uuid(p: Params, key: string): string {
  const v = str(p, key);
  if (!UUID.test(v)) throw new BridgeError(400, `${key} must be an id`);
  return v;
}

export function uuids(p: Params, key: string, max = 100): string[] {
  const v = p[key];
  if (
    !Array.isArray(v) ||
    v.length > max ||
    !v.every((x) => typeof x === "string" && UUID.test(x))
  ) {
    throw new BridgeError(400, `${key} must be a list of at most ${max} ids`);
  }
  return v as string[];
}

export function object(p: Params, key: string, optional = false): Params | undefined {
  const v = p[key];
  if (v === undefined && optional) return undefined;
  if (!v || typeof v !== "object" || Array.isArray(v)) {
    throw new BridgeError(400, `${key} must be an object`);
  }
  return v as Params;
}
