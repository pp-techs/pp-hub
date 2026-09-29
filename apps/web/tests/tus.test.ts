import { expect, test } from "vite-plus/test";
import { ApiError } from "../src/lib/api/client.ts";
import {
  CHUNK_MAX,
  CHUNK_TARGET_MS,
  MAX_FAILURES,
  startResumableUpload,
  type ResumableUploadOptions,
  type TusReply,
  type TusTransport,
} from "../src/lib/api/tus.ts";

const MiB = 1024 * 1024;

/** How one request goes wrong: the server keeps `keep` bytes, then answers `status`. */
interface Fault {
  keep?: number;
  status: number;
  headers?: Record<string, string>;
  body?: string;
}

/**
 * How one POST goes wrong. With `created`, the server made the upload before
 * failing, so the answer carries its Location and offset.
 */
interface CreateFault extends Fault {
  created?: boolean;
}

interface Upload {
  length: number;
  offset: number;
  data: Uint8Array | null;
  commits: number;
}

function reply(status: number, headers: Record<string, string> = {}, body = ""): TusReply {
  return { status, header: (name) => headers[name] ?? null, body };
}

/**
 * A tus server on a simulated link: `rate` bytes per virtual millisecond,
 * a clock that sleeps advance instantly, and scripted faults per request.
 */
class FakeServer implements TusTransport {
  clock = 0;
  uploads = new Map<string, Upload>();
  faults: Fault[] = [];
  createFaults: CreateFault[] = [];
  requests: string[] = [];
  patches: { offset: number; size: number; ms: number }[] = [];
  sleeps: number[] = [];
  /** Bytes the server accepted, summed over every request. */
  received = 0;
  /** Extra headers on the response that commits an upload (`Upload-Attachment`). */
  commitHeaders: Record<string, string> = {};
  private seq = 0;

  private readonly rate: number;

  constructor(rate = 10_000) {
    this.rate = rate;
  }

  async create(length: number, _metadata: string, body: Blob | undefined): Promise<TusReply> {
    this.requests.push("POST");
    const fault = this.createFaults.shift();
    if (fault && !fault.created) return reply(fault.status, fault.headers, fault.body);

    const url = `/api/v1/uploads/u${++this.seq}`;
    const u: Upload = { length, offset: 0, data: null, commits: 0 };
    this.uploads.set(url, u);
    if (body) await this.store(u, body, Math.min(fault?.keep ?? body.size, body.size));
    const headers = { Location: url, "Upload-Offset": String(u.offset) };
    if (fault) return reply(fault.status, { ...headers, ...fault.headers }, fault.body);
    // Creation-with-upload that completes the file commits before answering.
    if (body && u.offset === u.length) {
      u.commits += 1;
      return reply(201, { ...headers, ...this.commitHeaders });
    }
    return reply(201, headers);
  }

  async head(url: string): Promise<TusReply> {
    this.requests.push("HEAD");
    const u = this.uploads.get(url);
    if (!u) return reply(404);
    return reply(200, { "Upload-Offset": String(u.offset), "Upload-Length": String(u.length) });
  }

  async patch(
    url: string,
    offset: number,
    chunk: Blob,
    onSent: (bytes: number) => void,
  ): Promise<TusReply> {
    this.requests.push("PATCH");
    const u = this.uploads.get(url);
    if (!u) return reply(404);
    if (offset !== u.offset) return reply(409, { "Upload-Offset": String(u.offset) });

    const fault = this.faults.shift();
    const keep = Math.min(fault?.keep ?? chunk.size, chunk.size);
    const ms = keep / this.rate;
    this.clock += ms;
    this.patches.push({ offset, size: chunk.size, ms });
    await this.store(u, chunk, keep);
    onSent(keep);

    if (fault) return reply(fault.status, fault.headers, fault.body);
    if (u.offset === u.length) {
      u.commits += 1;
      return reply(204, { "Upload-Offset": String(u.offset), ...this.commitHeaders });
    }
    return reply(204, { "Upload-Offset": String(u.offset) });
  }

  /** Appends the first `keep` bytes of `chunk` at the upload's offset. */
  private async store(u: Upload, chunk: Blob, keep: number): Promise<void> {
    // `sized` files hand out stand-ins with no bytes behind them.
    if (chunk instanceof Blob && keep > 0) {
      u.data ??= new Uint8Array(u.length);
      u.data.set(new Uint8Array(await chunk.slice(0, keep).arrayBuffer()), u.offset);
    }
    u.offset += keep;
    this.received += keep;
  }

  async sleep(ms: number, signal: AbortSignal): Promise<void> {
    signal.throwIfAborted();
    this.sleeps.push(ms);
    this.clock += ms;
  }

  now(): number {
    return this.clock;
  }
}

function patterned(size: number): File {
  const bytes = new Uint8Array(size);
  for (let i = 0; i < size; i++) bytes[i] = (i * 31 + 7) % 251;
  return new File([bytes], "report.bin");
}

/** A file too large to hold in memory; only its size matters to the loop. */
function sized(size: number): File {
  return {
    name: "huge.bin",
    size,
    slice: (start = 0, end = size) => ({ size: end - start }),
  } as unknown as File;
}

function options(file: File, extra: Partial<ResumableUploadOptions> = {}): ResumableUploadOptions {
  return { file, target: { kind: "folder", spaceId: "s1" }, onProgress: () => {}, ...extra };
}

/**
 * Whether the server assembled exactly the file. A hand loop: deep equality
 * over millions of elements is what would make the test slow, not the upload.
 */
async function assembled(data: Uint8Array | null, file: File): Promise<boolean> {
  const want = new Uint8Array(await file.arrayBuffer());
  if (!data || data.length !== want.length) return false;
  for (let i = 0; i < want.length; i++) if (data[i] !== want[i]) return false;
  return true;
}

test("a chunk cut off mid-body resumes from what the server kept, never re-sending it", async () => {
  const server = new FakeServer();
  const file = patterned(10 * MiB);
  server.faults.push({ keep: 1 * MiB, status: 0 });

  await startResumableUpload(options(file), server).promise;

  const [u] = server.uploads.values();
  expect(await assembled(u.data, file)).toBe(true);
  expect(u.commits).toBe(1);
  // Every byte crossed the wire once: the retry started where the server
  // stopped, not where the failed chunk began.
  expect(server.received).toBe(file.size);
  expect(server.patches[1].offset).toBe(1 * MiB);
  // The chunk that failed was halved for the next attempt.
  expect(server.patches[1].size).toBeLessThanOrEqual(server.patches[0].size / 2);
});

test("a chunk a proxy timed out sizes the next one from what got through", async () => {
  // 24 KB/s behind a 60 s request deadline: the 3 MiB opening chunk is cut
  // after 1.44 MB, which is also the only honest measure of the link.
  const server = new FakeServer(24);
  const file = patterned(3 * MiB);
  server.faults.push({ keep: 60_000 * 24, status: 499 });

  await startResumableUpload(options(file), server).promise;

  // Halving alone would send 2 MiB — another 87 s, another timeout.
  for (const p of server.patches.slice(1)) expect(p.ms).toBeLessThan(60_000);
  expect(await assembled([...server.uploads.values()][0].data, file)).toBe(true);
});

test("progress counts from the start of the file, across a failed chunk", async () => {
  const server = new FakeServer();
  const file = patterned(6 * MiB);
  server.faults.push({ keep: 3 * MiB, status: 502 });
  const seen: number[] = [];

  await startResumableUpload(options(file, { onProgress: (n) => seen.push(n) }), server).promise;

  // The server kept everything that was sent, so the count never goes back.
  expect(seen).toEqual([...seen].sort((a, b) => a - b));
  expect(seen.at(-1)).toBe(file.size);
});

test("failures that still move bytes never exhaust the retries", async () => {
  const server = new FakeServer();
  const file = patterned(4 * MiB);
  // More failures in a row than the limit allows, but each one lands 128 KiB.
  for (let i = 0; i < MAX_FAILURES * 2; i++) server.faults.push({ keep: 128 * 1024, status: 504 });

  await startResumableUpload(options(file), server).promise;

  const [u] = server.uploads.values();
  expect(await assembled(u.data, file)).toBe(true);
});

test("gives up after consecutive failures that gain nothing, with the last status", async () => {
  const server = new FakeServer();
  for (let i = 0; i < 50; i++) server.faults.push({ keep: 0, status: 502 });

  const error = await startResumableUpload(options(patterned(MiB)), server).promise.catch(
    (e: unknown) => e,
  );

  expect(error).toBeInstanceOf(ApiError);
  expect((error as ApiError).status).toBe(502);
  expect(server.patches).toHaveLength(MAX_FAILURES);
  // Backoff grows between attempts.
  expect(server.sleeps).toEqual([...server.sleeps].sort((a, b) => a - b));
  expect(server.sleeps[0]).toBeLessThan(server.sleeps.at(-1)!);
});

test("a rate-limited chunk waits out Retry-After", async () => {
  const server = new FakeServer();
  server.faults.push({ keep: 0, status: 429, headers: { "Retry-After": "20" } });

  await startResumableUpload(options(patterned(MiB)), server).promise;

  expect(server.sleeps[0]).toBeGreaterThanOrEqual(20_000);
});

test("a refusal fails at once with the server's sentence", async () => {
  const server = new FakeServer();
  server.create = async () =>
    reply(
      413,
      {},
      JSON.stringify({ title: "Too large", detail: "The space is full.", status: 413 }),
    );

  const error = await startResumableUpload(options(patterned(MiB)), server).promise.catch(
    (e: unknown) => e,
  );

  expect(error).toBeInstanceOf(ApiError);
  expect((error as ApiError).status).toBe(413);
  expect((error as ApiError).message).toBe("The space is full.");
  expect(server.sleeps).toEqual([]);
});

test("abort stops without terminating, and the same URL picks up where it stopped", async () => {
  const server = new FakeServer();
  const file = patterned(10 * MiB);
  let url: string | undefined;
  const first = startResumableUpload(
    options(file, {
      onUrl: (u) => {
        url = u;
      },
      onProgress: (n) => {
        if (n > 0) first.abort();
      },
    }),
    server,
  );

  const error = await first.promise.catch((e: unknown) => e);
  expect(error).toBeInstanceOf(ApiError);
  expect((error as ApiError).status).toBe(0);
  expect((error as ApiError).message).toBe("Cancelled");

  const kept = server.uploads.get(url!)!.offset;
  expect(kept).toBeGreaterThan(0);
  server.patches = [];

  await startResumableUpload(options(file, { uploadUrl: url }), server).promise;

  expect(server.patches[0].offset).toBe(kept);
  expect(await assembled(server.uploads.get(url!)!.data, file)).toBe(true);
});

test("a remembered upload the server no longer knows starts over", async () => {
  const server = new FakeServer();
  const urls: string[] = [];

  await startResumableUpload(
    options(patterned(MiB), { uploadUrl: "/api/v1/uploads/expired", onUrl: (u) => urls.push(u) }),
    server,
  ).promise;

  expect(server.requests.slice(0, 2)).toEqual(["HEAD", "POST"]);
  expect(urls).toHaveLength(1);
  expect(server.uploads.get(urls[0])!.commits).toBe(1);
});

test("a commit refused as locked is retried later with an empty PATCH, not the bytes", async () => {
  const server = new FakeServer();
  const file = patterned(2 * MiB);
  let url: string | undefined;
  server.faults.push({
    status: 423,
    body: JSON.stringify({
      title: "Locked",
      detail: "The file is open in the editor.",
      status: 423,
    }),
  });

  const error = await startResumableUpload(
    options(file, {
      onUrl: (u) => {
        url = u;
      },
    }),
    server,
  ).promise.catch((e: unknown) => e);
  expect((error as ApiError).status).toBe(423);
  expect((error as ApiError).message).toBe("The file is open in the editor.");

  server.patches = [];
  server.received = 0;
  await startResumableUpload(options(file, { uploadUrl: url }), server).promise;

  expect(server.patches).toEqual([{ offset: file.size, size: 0, ms: 0 }]);
  expect(server.uploads.get(url!)!.commits).toBe(1);
});

test("a zero-byte file is created and committed in one request", async () => {
  const server = new FakeServer();

  await startResumableUpload(options(new File([], "empty.txt")), server).promise;

  const [u] = server.uploads.values();
  expect(u.commits).toBe(1);
  expect(server.requests).toEqual(["POST"]);
});

test("an upload resolves with the reply that committed it, whichever request that was", async () => {
  for (const file of [patterned(200 * 1024), patterned(10 * MiB)]) {
    const server = new FakeServer();
    server.commitHeaders = { "Upload-Attachment": "sha256-abc" };

    const committed = await startResumableUpload(options(file), server).promise;

    expect(committed.header("Upload-Attachment")).toBe("sha256-abc");
  }
});

test("an attachment names its document and nothing about a folder", async () => {
  const server = new FakeServer();
  let sent = "";
  const create = server.create.bind(server);
  server.create = (length, metadata, body) => {
    sent = metadata;
    return create(length, metadata, body);
  };

  await startResumableUpload(
    options(new File(["x"], "pic.png"), { target: { kind: "attachment", documentId: "doc1" } }),
    server,
  ).promise;

  const keys = sent.split(",").map((pair) => pair.split(" ")[0]);
  expect(keys).toEqual(["filename", "attach_to"]);
  expect(sent).toContain(`attach_to ${btoa("doc1")}`);
});

test("a small file goes up in one request", async () => {
  const server = new FakeServer();
  const file = patterned(200 * 1024);

  await startResumableUpload(options(file), server).promise;

  const [u] = server.uploads.values();
  expect(await assembled(u.data, file)).toBe(true);
  expect(u.commits).toBe(1);
  expect(server.requests).toEqual(["POST"]);
});

test("a small file whose POST body was cut short continues from what arrived", async () => {
  const server = new FakeServer();
  const file = patterned(200 * 1024);
  server.createFaults.push({ created: true, keep: 64 * 1024, status: 400 });

  await startResumableUpload(options(file), server).promise;

  const [u] = server.uploads.values();
  expect(await assembled(u.data, file)).toBe(true);
  expect(u.commits).toBe(1);
  expect(server.requests).toEqual(["POST", "HEAD", "PATCH"]);
  expect(server.received).toBe(file.size);
});

test("a small file whose commit failed transiently is committed again without its bytes", async () => {
  const server = new FakeServer();
  const file = patterned(200 * 1024);
  server.createFaults.push({ created: true, status: 503 });

  await startResumableUpload(options(file), server).promise;

  const [u] = server.uploads.values();
  expect(u.commits).toBe(1);
  expect(server.requests).toEqual(["POST", "HEAD", "PATCH"]);
  expect(server.patches).toEqual([{ offset: file.size, size: 0, ms: 0 }]);
  expect(server.received).toBe(file.size);
});

test("a small file refused as locked at commit is remembered for a later retry", async () => {
  const server = new FakeServer();
  const file = patterned(200 * 1024);
  let url: string | undefined;
  server.createFaults.push({
    created: true,
    status: 423,
    body: JSON.stringify({
      title: "Locked",
      detail: "The file is open in the editor.",
      status: 423,
    }),
  });

  const error = await startResumableUpload(
    options(file, {
      onUrl: (u) => {
        url = u;
      },
    }),
    server,
  ).promise.catch((e: unknown) => e);
  expect((error as ApiError).status).toBe(423);
  expect(url).toBeDefined();

  server.requests = [];
  await startResumableUpload(options(file, { uploadUrl: url }), server).promise;

  expect(server.requests).toEqual(["HEAD", "PATCH"]);
  expect(server.uploads.get(url!)!.commits).toBe(1);
  expect(server.received).toBe(file.size);
});

test("a POST that failed before creating anything is sent again", async () => {
  const server = new FakeServer();
  const file = patterned(200 * 1024);
  server.createFaults.push({ status: 503 });

  await startResumableUpload(options(file), server).promise;

  expect(server.requests).toEqual(["POST", "POST"]);
  expect(server.uploads.size).toBe(1);
  expect([...server.uploads.values()][0].commits).toBe(1);
});

test("on a slow link chunks settle near the target duration", async () => {
  // 100 KB/s: the 4 MiB opening chunk alone takes 42 s.
  const server = new FakeServer(100);

  await startResumableUpload(options(sized(60 * MiB)), server).promise;

  const settled = server.patches.slice(2, -1);
  expect(settled.length).toBeGreaterThan(0);
  for (const p of settled) {
    expect(p.ms).toBeGreaterThan(CHUNK_TARGET_MS * 0.5);
    expect(p.ms).toBeLessThan(CHUNK_TARGET_MS * 1.5);
  }
});

test("on a fast link chunks grow to the ceiling and no further", async () => {
  // 100 MB/s would fill a 15 s chunk with 1.5 GB.
  const server = new FakeServer(100_000);

  await startResumableUpload(options(sized(2 * 1024 * MiB)), server).promise;

  const sizes = server.patches.map((p) => p.size);
  expect(Math.max(...sizes)).toBe(CHUNK_MAX);
  // Growth is gradual: never more than double the chunk before it.
  for (let i = 1; i < sizes.length; i++) expect(sizes[i]).toBeLessThanOrEqual(sizes[i - 1] * 2);
});
