// Resumable uploads: a tus 1.0.0 client (core + creation + termination).
//
// A file used to go up as one multipart POST, which a proxy that bounds
// request duration (Traefik's readTimeout) or body size (Cloudflare's 100 MB)
// cut off every time for a large enough file. Here a file is created as an
// upload resource and sent as a sequence of PATCHes sized to finish well
// inside any such bound; a dropped chunk costs only what the server did not
// receive, because the server keeps every byte that arrived and the client
// asks for its offset before sending more.

import { isNetworkError, isTimeoutError } from "ky";
import { api, ApiError, unauthorized } from "./client";

const TUS_VERSION = "1.0.0";
const CREATE_PATH = "/api/v1/uploads";
const CHUNK_TYPE = "application/offset+octet-stream";

/**
 * Chunk sizing. Each PATCH aims at `CHUNK_TARGET_MS` of wall time, well under
 * the 60 s a proxy commonly allows a request body, and the size follows the
 * measured throughput toward it. The ceiling stays under Cloudflare's 100 MB
 * body cap; the floor keeps a crawling link from paying a round trip per
 * handful of bytes.
 */
export const CHUNK_MIN = 256 * 1024;
export const CHUNK_MAX = 64 * 1024 * 1024;
export const CHUNK_INITIAL = 4 * 1024 * 1024;
export const CHUNK_TARGET_MS = 15_000;

/**
 * Wait before the nth consecutive retry; the last entry repeats. Several
 * seconds in total, so a proxy restart or a brief network drop is ridden out
 * without the user noticing.
 */
export const RETRY_DELAYS_MS = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000] as const;

/**
 * Consecutive retryable failures, with no byte gained in between, before the
 * upload gives up. Any failure that still moved the server's offset resets
 * the count: a link that keeps dropping but keeps making progress finishes.
 */
export const MAX_FAILURES = 8;

/** A `Retry-After` longer than this is not waited out in full. */
const MAX_RETRY_AFTER_MS = 5 * 60_000;

/**
 * A PATCH still running after this long is treated as a dropped connection.
 * Harmless when wrong — the server keeps what arrived and the loop resumes
 * from there — and it bounds how long a silently dead link can hang a row.
 */
const PATCH_TIMEOUT_MS = 120_000;
const REQUEST_TIMEOUT_MS = 30_000;

/** What the upload loop reads from one response. */
export interface TusReply {
  /** 0 when no response arrived: a dropped connection or a timeout. */
  status: number;
  header(name: string): string | null;
  /**
   * The body when it is JSON (a problem document), otherwise "". A proxy's
   * HTML error page is not a message anyone should read in the tray.
   */
  body: string;
}

/**
 * Everything the upload loop touches outside itself: the three requests, the
 * wait between retries and the clock it measures throughput with. The browser
 * implementation is the default; tests drive the loop against a simulated
 * server instead.
 */
export interface TusTransport {
  /** `body`, when given, is the whole file, sent with the POST (creation-with-upload). */
  create(
    length: number,
    metadata: string,
    body: Blob | undefined,
    signal: AbortSignal,
  ): Promise<TusReply>;
  head(url: string, signal: AbortSignal): Promise<TusReply>;
  patch(
    url: string,
    offset: number,
    chunk: Blob,
    onSent: (bytes: number) => void,
    signal: AbortSignal,
  ): Promise<TusReply>;
  sleep(ms: number, signal: AbortSignal): Promise<void>;
  /** Monotonic milliseconds. */
  now(): number;
}

/**
 * Where the bytes land. A replacement names its node and nothing else — the
 * server takes the space and folder from the file it replaces. An attachment
 * belongs to a document: it never appears in a folder listing, and the
 * completing response names its blob in `Upload-Attachment`.
 */
export type UploadTarget =
  | {
      kind: "folder";
      spaceId: string;
      /** Absent means the space's top level. */ parentId?: string;
    }
  | { kind: "replace"; nodeId: string }
  | { kind: "attachment"; documentId: string };

export interface ResumableUploadOptions {
  file: File;
  target: UploadTarget;
  /**
   * An upload this file already started, from an earlier `onUrl`. The server
   * is asked how far it got and the upload continues from there; if it no
   * longer knows the upload (expired, or its commit failed), a fresh one
   * starts.
   */
  uploadUrl?: string;
  /** Absolute bytes: the server's offset plus what the current chunk has sent. */
  onProgress: (loaded: number) => void;
  /** The upload's address, once the server has created it. */
  onUrl?: (url: string) => void;
}

export interface ResumableUpload {
  /**
   * Resolves once the server has committed the file, with the response that
   * committed it (a 201 creation-with-upload or the final 204 PATCH), so a
   * caller can read what the server said about the result. Rejects with an
   * `ApiError`: the server's refusal (its `detail` as the message), the last
   * transient failure once retries run out, or status 0 "Cancelled" after
   * `abort`.
   */
  promise: Promise<TusReply>;
  /**
   * Stops without terminating: the server keeps what it received, so passing
   * the same `uploadUrl` later resumes. No callback fires after this.
   */
  abort: () => void;
}

export function startResumableUpload(
  options: ResumableUploadOptions,
  transport: TusTransport = browserTransport,
): ResumableUpload {
  const controller = new AbortController();
  const promise = upload(options, transport, controller.signal).catch((e: unknown) => {
    throw controller.signal.aborted ? new ApiError(0, "Cancelled") : e;
  });
  return { promise, abort: () => controller.abort() };
}

/**
 * Frees what the server staged for an upload nobody will resume.
 * Fire-and-forget: an upload that is never terminated still expires.
 */
export function terminateUpload(url: string): void {
  void api
    .delete(url, {
      headers: { "Tus-Resumable": TUS_VERSION },
      retry: 0,
      timeout: REQUEST_TIMEOUT_MS,
    })
    .catch(() => undefined);
}

/**
 * The next chunk size after one that sent `sent` bytes in `elapsedMs`: the
 * size that would take `CHUNK_TARGET_MS` at the measured rate. One sample
 * moves it at most 2× up and 4× down — a single fast chunk may have ridden a
 * burst, while a slow one is already a warning that the next may not fit
 * inside a proxy's timeout.
 */
export function nextChunkSize(current: number, sent: number, elapsedMs: number): number {
  if (sent <= 0 || elapsedMs <= 0) return current;
  const ideal = (sent / elapsedMs) * CHUNK_TARGET_MS;
  const bounded = Math.min(current * 2, Math.max(current / 4, ideal));
  return Math.min(CHUNK_MAX, Math.max(CHUNK_MIN, Math.round(bounded)));
}

/** Statuses worth retrying: no response at all, or a proxy/gateway failure. */
export function isTransient(status: number): boolean {
  return (
    status === 0 ||
    status === 408 ||
    status === 429 ||
    status === 499 ||
    status === 500 ||
    (status >= 502 && status <= 504) ||
    (status >= 520 && status <= 527)
  );
}

async function upload(
  o: ResumableUploadOptions,
  io: TusTransport,
  signal: AbortSignal,
): Promise<TusReply> {
  const size = o.file.size;
  const report = (loaded: number) => {
    if (!signal.aborted) o.onProgress(loaded);
  };

  let url = o.uploadUrl;
  /** The server's offset; undefined when it has to be asked for. */
  let offset: number | undefined;
  /** Furthest offset the server has confirmed, to tell progress from a stall. */
  let furthest = -1;
  /**
   * Whether the server has answered for `url` in this run. Until it has, a
   * 404 means the remembered upload is gone and a fresh one should start;
   * after, it means the upload vanished underneath us, which is an error.
   */
  let known = false;
  let chunk = CHUNK_INITIAL;
  let failures = 0;
  /**
   * A chunk that failed transiently, kept until the HEAD after it says how
   * much of it arrived. A proxy that times a request out still passed on
   * what it had read, so that is a real measure of the link — halving alone
   * can take several more timeouts to get under the proxy's deadline.
   */
  let cut: { from: number; elapsedMs: number } | undefined;

  const learn = (next: number) => {
    offset = next;
    known = true;
    if (next > furthest) {
      furthest = next;
      failures = 0;
    }
    report(next);
  };

  const fail = async (reply: TusReply, wait: boolean) => {
    failures += 1;
    // The status is the whole story here: after this many attempts the body
    // is whatever the last proxy in the way said.
    if (failures >= MAX_FAILURES) throw new ApiError(reply.status, "");
    if (wait) await io.sleep(retryDelay(failures, reply), signal);
  };

  for (;;) {
    signal.throwIfAborted();

    if (url === undefined) {
      // A file no bigger than the smallest chunk rides on the POST itself:
      // one request instead of two, which is what keeps a folder of small
      // files inside the API's per-address rate limit. A zero-byte file is
      // committed by that same request.
      const body = size <= CHUNK_MIN ? o.file : undefined;
      const reply = await io.create(size, metadata(o), body, signal);
      const location = reply.header("Location");
      if (location) {
        // The upload exists whatever the status says — a commit refused as
        // locked keeps it, and a body cut short keeps what arrived — so it is
        // ours to resume, now and on a later retry.
        url = uploadPath(location);
        known = true;
        if (!signal.aborted) o.onUrl?.(url);
      }
      if (reply.status === 201) {
        if (url === undefined) throw new Error("tus: 201 Created without a Location");
        const next = parseOffset(reply) ?? 0;
        learn(next);
        // With a body, a 201 at full length means the server committed first.
        if (body && next >= size) return reply;
        continue;
      }
      // A body cut short answers 400 alongside the Location; like a transient
      // failure, that resumes from whatever offset the server reports.
      const resumable = url !== undefined && reply.status === 400;
      if (!resumable && !isTransient(reply.status)) throw refusal(reply);
      await fail(reply, true);
      continue;
    }

    if (offset === undefined) {
      const reply = await io.head(url, signal);
      if (reply.status === 200) {
        const next = parseOffset(reply);
        if (next === undefined) throw new Error("tus: HEAD without an Upload-Offset");
        if (cut && next > cut.from && cut.elapsedMs > 0) {
          // The size that takes CHUNK_TARGET_MS at the rate the cut chunk ran.
          const measured = Math.round(((next - cut.from) / cut.elapsedMs) * CHUNK_TARGET_MS);
          chunk = Math.min(chunk, Math.max(CHUNK_MIN, measured));
        }
        cut = undefined;
        learn(next);
        continue;
      }
      if ((reply.status === 404 || reply.status === 410) && !known) {
        url = undefined;
        continue;
      }
      if (!isTransient(reply.status)) throw refusal(reply);
      await fail(reply, true);
      continue;
    }

    // At offset == size this sends an empty body. That confirms the commit
    // when the response to the final chunk was lost, and retries a commit the
    // server refused as locked (423) or failed (5xx) — it keeps those uploads
    // at full length rather than deleting them.
    const from = offset;
    const end = Math.min(size, from + chunk);
    const started = io.now();
    const reply = await io.patch(
      url,
      from,
      o.file.slice(from, end),
      (sent) => report(from + sent),
      signal,
    );
    if (reply.status === 204) {
      chunk = nextChunkSize(chunk, end - from, io.now() - started);
      const next = parseOffset(reply) ?? end;
      learn(next);
      // A 204 at full length means the server committed before answering.
      if (next >= size) return reply;
      continue;
    }

    // Whatever went wrong, the server may hold more (or less) than we think.
    offset = undefined;
    if (reply.status === 409) {
      // The mismatch answer carries the server's offset, which spares a HEAD.
      // Only an offset past the furthest one seen counts as progress.
      const current = parseOffset(reply);
      if (current === undefined || current <= furthest) await fail(reply, false);
      if (current !== undefined) learn(current);
      continue;
    }
    if (!isTransient(reply.status)) throw refusal(reply);
    cut = { from, elapsedMs: io.now() - started };
    chunk = Math.max(CHUNK_MIN, Math.floor(chunk / 2));
    await fail(reply, true);
  }
}

function refusal(reply: TusReply): ApiError {
  return new ApiError(reply.status, reply.body);
}

function retryDelay(failures: number, reply: TusReply): number {
  const backoff = RETRY_DELAYS_MS[Math.min(failures, RETRY_DELAYS_MS.length) - 1];
  return Math.max(backoff, Math.min(MAX_RETRY_AFTER_MS, retryAfter(reply.header("Retry-After"))));
}

/** `Retry-After` in milliseconds: delta-seconds or an HTTP date. */
function retryAfter(value: string | null): number {
  if (!value) return 0;
  const seconds = Number(value);
  if (Number.isFinite(seconds)) return Math.max(0, seconds * 1000);
  const at = Date.parse(value);
  return Number.isNaN(at) ? 0 : Math.max(0, at - Date.now());
}

function parseOffset(reply: TusReply): number | undefined {
  const value = reply.header("Upload-Offset");
  if (value === null) return undefined;
  const n = Number(value);
  return Number.isSafeInteger(n) && n >= 0 ? n : undefined;
}

/** `Upload-Metadata`: comma-separated `key base64(value)` pairs. */
function metadata(o: ResumableUploadOptions): string {
  const pairs: [string, string][] = [["filename", o.file.name]];
  const target = o.target;
  switch (target.kind) {
    case "folder":
      pairs.push(["space_id", target.spaceId]);
      if (target.parentId) pairs.push(["parent_id", target.parentId]);
      break;
    case "replace":
      pairs.push(["node_id", target.nodeId]);
      break;
    case "attachment":
      pairs.push(["attach_to", target.documentId]);
      break;
  }
  return pairs.map(([key, value]) => `${key} ${base64(value)}`).join(",");
}

/** Standard base64 of the UTF-8 bytes; `btoa` alone only takes Latin-1. */
function base64(value: string): string {
  let binary = "";
  for (const byte of new TextEncoder().encode(value)) binary += String.fromCharCode(byte);
  return btoa(binary);
}

/**
 * The upload's address as a same-origin path. The server may answer with an
 * absolute URL built from its own view of the host — behind a proxy, the
 * internal one — but every upload lives on this origin, and ky's `prefix`
 * only composes with a path. Resolving against the create URL keeps a
 * relative answer correct; the placeholder origin never leaves this function.
 */
function uploadPath(location: string): string {
  const url = new URL(location, new URL(CREATE_PATH, "http://origin.invalid"));
  return url.pathname + url.search;
}

// ─── Browser transport ──────────────────────────────────────────────────────

const NO_REPLY: TusReply = { status: 0, header: () => null, body: "" };

const browserTransport: TusTransport = {
  create: (length, metadata, body, signal) =>
    request(() =>
      api.post(CREATE_PATH, {
        headers: {
          "Tus-Resumable": TUS_VERSION,
          "Upload-Length": String(length),
          "Upload-Metadata": metadata,
          ...(body ? { "Content-Type": CHUNK_TYPE } : {}),
        },
        body,
        retry: 0,
        timeout: REQUEST_TIMEOUT_MS,
        signal,
      }),
    ),
  head: (url, signal) =>
    request(() =>
      api.head(url, {
        headers: { "Tus-Resumable": TUS_VERSION },
        cache: "no-store",
        retry: 0,
        timeout: REQUEST_TIMEOUT_MS,
        signal,
      }),
    ),
  patch: patchChunk,
  sleep,
  now: () => performance.now(),
};

/**
 * One request through the shared client, with ky's own retries off — the
 * upload loop owns that policy, and two stacked backoffs would multiply.
 * A 401 still throws from the client's hook, and an abort still rejects.
 */
async function request(send: () => Promise<Response>): Promise<TusReply> {
  let res: Response;
  try {
    res = await send();
  } catch (e) {
    if (isNetworkError(e) || isTimeoutError(e)) return NO_REPLY;
    throw e;
  }
  const json = (res.headers.get("Content-Type") ?? "").includes("json");
  const body = json ? await res.text() : "";
  return { status: res.status, header: (name) => res.headers.get(name), body };
}

/**
 * The PATCH goes through XMLHttpRequest because `fetch` (and so ky) exposes
 * no upload progress. A dropped or stalled connection resolves as status 0
 * rather than rejecting: the server kept some prefix of the chunk, and the
 * loop asks how much.
 */
function patchChunk(
  url: string,
  offset: number,
  chunk: Blob,
  onSent: (bytes: number) => void,
  signal: AbortSignal,
): Promise<TusReply> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason);
      return;
    }
    const xhr = new XMLHttpRequest();
    const stop = () => xhr.abort();
    const settle = () => signal.removeEventListener("abort", stop);

    xhr.upload.addEventListener("progress", (e) => onSent(e.loaded));
    xhr.addEventListener("load", () => {
      settle();
      if (xhr.status === 401) {
        reject(unauthorized());
        return;
      }
      const json = (xhr.getResponseHeader("Content-Type") ?? "").includes("json");
      resolve({
        status: xhr.status,
        header: (name) => xhr.getResponseHeader(name),
        body: json ? xhr.responseText : "",
      });
    });
    xhr.addEventListener("error", () => {
      settle();
      resolve(NO_REPLY);
    });
    xhr.addEventListener("timeout", () => {
      settle();
      resolve(NO_REPLY);
    });
    xhr.addEventListener("abort", () => {
      settle();
      reject(signal.reason);
    });
    signal.addEventListener("abort", stop, { once: true });

    xhr.open("PATCH", url);
    xhr.timeout = PATCH_TIMEOUT_MS;
    xhr.setRequestHeader("Tus-Resumable", TUS_VERSION);
    xhr.setRequestHeader("Upload-Offset", String(offset));
    xhr.setRequestHeader("Content-Type", CHUNK_TYPE);
    xhr.send(chunk);
  });
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason);
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal.reason);
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}
