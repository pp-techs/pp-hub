# Preview Pipeline

Background job workers generate a small preview blob per file version. The
browser fetches it from `GET /api/v1/nodes/{id}/preview?kind=…`.

Images, PDFs, video and office documents all get a real rasterized
`thumbnail`; text files get a `text` snippet. The thumbnail is what the grid
cards and the details panel show — both fall back to the file-type icon
(`NodeIcon`) when there is none, since a blank frame reads as broken whether
the file simply has no thumbnail (text, archives) or the job has not run yet.

Markdown files are the exception to both: no artifact is stored for them. The
grid card and the details panel render the document's first screen at half
scale with the real read-only renderer (`MarkdownThumbnail`,
`components/markdown-document-view`), fetched from `/nodes/{id}/document`
rather than `/preview`. A PNG would need a font covering every script in the
drive (the bundled Go fonts lack Vietnamese); the browser already has them. The
document fetch and editor start only once the card is within 200px of the
viewport, and the file-type icon stands in when the document cannot be read.

How a thumbnail fills its frame is decided by `thumbnailFit`: pictures
(`image/*`, `video/*`) crop to fill, pages (PDF, office) letterbox. PDFs sit
with the pages even though they render through the same rasterizer as images —
cropping a portrait A4 to a landscape card shows a horizontal strip of its
middle, which is the part that identifies a document least. It is capped at 256px, so it is far too small for the full-size viewer —
that renders the original bytes from `GET /api/v1/nodes/{id}/content` instead,
see [Serving the original file](#serving-the-original-file).

## Two kinds, deliberately separate

The pipeline used to call both of these "kind", which forced the Go worker and
the TypeScript client to maintain identical maps or every request 404'd. They
are now distinct:

```
jobs.kind = how to render        │  previews.kind = what was produced
  render:image   ─┐              │
  render:svg     ─┤              │
  render:pdf     ─┼──────────────────> thumbnail   (PNG, longest side ≤ 256px)
  render:video   ─┤              │
  render:office  ─┘              │
  snippet:text   ────────────────────> text        (first 4KB, verbatim)
  extract:text   ────────────────────> node_texts row (M27, not a preview)
```

- A **job kind** is a routing key. It decides which process may claim the work,
  and nothing outside `internal/preview` needs to know it exists.
- A **preview kind** is the artifact the browser asks for. Everything that
  rasterizes produces the same one.

`kindForMime` picks the job kind; `previewKindFor` maps that to the artifact.
The client's `previewKind` (`apps/web/src/lib/api/utils.ts`) answers the second
question only, so it no longer mirrors anything on the server.

| MIME                                                              | job kind        | preview kind |
| ----------------------------------------------------------------- | --------------- | ------------ |
| `image/png`, `image/jpeg`, `image/gif`, `image/webp`              | `render:image`  | `thumbnail`  |
| `image/svg+xml`                                                   | `render:svg`    | `thumbnail`  |
| `application/pdf`                                                 | `render:pdf`    | `thumbnail`  |
| `video/mp4`, `video/webm`, `video/quicktime`                      | `render:video`  | `thumbnail`  |
| `.docx` `.doc` `.odt` `.xlsx` `.xls` `.ods` `.pptx` `.ppt` `.odp` | `render:office` | `thumbnail`  |
| `application/rtf`, `text/*`                                       | `snippet:text`  | `text`       |
| anything else                                                     | —               | none         |

Both sides strip MIME parameters (`; charset=utf-8`) before matching.

The office set is deliberately narrower than `httpapi.isOfficeMime`, which
decides what the _editor_ opens: that list includes `application/octet-stream`
as a fallback and `text/csv`, neither of which should have a document
speculatively rendered. `.epub` is a zip the editor handles but a rendered
"cover" would misrepresent, and `.rtf` keeps its text snippet.

## Flow

1. `fs.PutFile` commits a new version and fires `fs.SetVersionHook`
   (wired in `apps/server/cmd/server/main.go`).
2. `preview.Worker.Enqueue` maps the version's MIME to a job kind via
   `kindForMime`. **An unmapped MIME enqueues nothing.**
3. A worker that registered a matching `Renderer` claims the job with
   `FOR UPDATE SKIP LOCKED`, renders it, and upserts a `previews` row keyed by
   `(node_version_id, preview_kind)`.
4. `handlePreview` looks up that `(current_version_id, kind)` pair.

## Text extraction

Content search (M27) rides the same `jobs` table and claim loop, but its job
produces a `node_texts` row instead of a preview blob
(`internal/preview/extract.go`, migration `0016_content_search.sql`). An
extraction kind is a routing key exactly like a render kind: a process claims
only the kinds it registered an `Extractor` for.

- **`extract:text`** covers `text/markdown` — parsed and flattened by
  `mdoc.PlainText`, so the index and its snippets carry the words and not the
  markdown syntax — and `text/plain`. Both are pure Go and run in
  `cmd/server`; the worker is not needed.
- The version hook enqueues it next to the preview job, and a restored older
  version (which commits nothing new) is re-enqueued by `fs.SetRestoreHook`
  (`Worker.Reindex`).
- The row holds the text of the node's **current** version, normalised,
  capped at 256 KiB and folded by `internal/textsearch` (lower-cased, accents
  removed, rune for rune, so Vietnamese matches without the `unaccent`
  extension). The upsert is guarded so a late job for a superseded version
  cannot overwrite newer text, and search joins on
  `nodes.current_version_id`, so a row that has not caught up is not matched
  rather than served stale.
- **Office documents are not extracted yet.** They will need their own kind,
  registered in `cmd/worker` beside the converter and added to the MIME map;
  storage, backfill and search need no change.

## Where rendering happens

Two binaries share the `jobs` table. `internal/bootstrap` builds the config,
pool, queries and blob store for both, so the entrypoints cannot drift.

```
┌─ cmd/server ───────────────┐      ┌─ cmd/worker (optional) ───────┐
│ HTTP API                   │      │ no HTTP listener              │
│ renderers:                 │      │ renderers:                    │
│   render:image  (pure Go)  │      │   render:pdf    → pdftoppm    │
│   render:svg    (pure Go)  │      │   render:video  → ffmpeg      │
│   snippet:text  (pure Go)  │      │   render:office → OnlyOffice  │
│ owns migrations            │      │ waits for schema              │
└──────────┬─────────────────┘      └───────────┬───────────────────┘
           └──────────────┬─────────────────────┘
                          ▼
             jobs table — ClaimJobs(kinds[], n)
```

`pdftoppm` and `ffmpeg` parse hostile documents in memory-unsafe C with a long
CVE history. In-process, that parsing would sit beside the session key and the
connection pool; in `cmd/worker` it sits in a container with no listener, no
capabilities and a read-only root. That containment — not the smaller API
image — is why the split exists.

Only `cmd/server` runs migrations. `cmd/worker` calls `store.AwaitSchema`,
because two processes racing DDL on a cold start can both read
`schema_migrations` as empty and apply the same file.

## Why the worker is optional

`ClaimJobs` filters on the kinds the calling process registered renderers for:

```sql
WHERE status = 'pending' AND run_at <= now() AND kind = ANY(@kinds::text[])
```

An install with no worker never claims a `render:pdf`, so it never fails one
either — the job stays `pending` indefinitely and drains whenever a worker
appears. Without this filter the API server would claim the job, fail it five
times and mark it `failed` permanently, since `Enqueue` only ever fires on
upload.

The kinds set is **derived, not configured**: `NewWorker` takes renderers and
computes `Kinds()` from them, and each renderer's constructor returns `nil`
when its binary is missing. A worker image built without ffmpeg therefore
advertises no `render:video` and leaves that work for one that has it. The SQL
half is asserted by `TestClaimJobsFiltersByKind`, the Go half by
`TestWorkerKindsAreDerivedFromRenderers`.

## Recovering abandoned jobs

`ClaimJobs` flips a row to `running`, and only the worker holding it ever moves
it again. Lose that worker — OOM, container stop, a deploy mid-render — and the
job is stranded in `running` forever: never retried, never failed, invisible in
every listing. `Worker.reap` runs at the top of each tick and is the only thing
that puts those rows back.

A job is stale once it has been `running` for longer than `jobLease` (10
minutes), which cannot race a live worker: `renderTimeout` bounds the external
binaries at 30s and the rest is just spooling bytes. Jobs that have already
burned `maxAttempts` go to `failed` rather than being reclaimed forever, and
`last_error` records why.

This is why `ClaimJobs` also sets `run_at = now()`: for a `running` row that
column means _claimed at_. Without it a backfilled job whose `run_at` is hours
old would look stale the instant it was claimed, and a second worker could
start rendering it alongside the first.

Reaping is deliberately **not** filtered by the worker's kinds — a stranded
`render:pdf` must be recoverable by a process that cannot render PDFs, or
losing the only capable worker also loses the ability to notice.

## Renderers

Both external renderers spool to a temp file first — `pdftoppm` reads the
trailer to find the page tree, and `ffmpeg`'s fast seek needs random access —
and both funnel their output through `imageThumbnail`, so the 256px cap and the
PNG encoding are defined exactly once.

|        | PDF                                                                  | Video                                                       |
| ------ | -------------------------------------------------------------------- | ----------------------------------------------------------- |
| Probe  | —                                                                    | `ffprobe -show_entries format=duration`                     |
| Render | `pdftoppm -png -f 1 -l 1 -singlefile -scale-to-x 512 -scale-to-y -1` | `ffmpeg -ss <t> -i … -frames:v 1 -c:v png -f image2pipe -`  |
| Seek   | n/a                                                                  | `min(duration × 0.10, 10s)` — frame 0 is often a black fade |
| Retry  | none                                                                 | once at `t=0`, in case the duration probe overshot          |

Every exec goes through `internal/preview/exec.go`: a 30s `CommandContext`
deadline, a 16MB cap on captured output, a 512MB cap on spooled input, and a
per-job temp dir removed on return.

### SVG

`SVGRenderer` parses and rasterizes with `oksvg`/`rasterx` — both pure Go, so
like `ImageRenderer` it runs in `cmd/server` itself rather than needing
`cmd/worker`. Scaling happens in the rasterizer via `SvgIcon.SetTarget` rather
than as a resample pass afterwards, but the output still funnels through the
same 256px-cap, PNG-encode step every other rasterizer uses.

This is also why an SVG can be thumbnailed while its original bytes stay
`attachment`-only from `/content` (see below): rasterizing only ever walks
parsed path data and paints pixels, so an embedded `<script>` or external
`href` never executes or gets fetched — unlike handing a browser the raw
markup to render as a document.

`oksvg`'s color parser only understands literal values — it hard-errors the
instant it sees the bare token `currentColor` or any `var()`/`color-mix()`
call, even a `var()` naming a plain hex color declared right there in the
file. Both are common enough to matter: `currentColor` is how icon sets like
Feather/Lucide theme fill/stroke, and a handful of custom properties on the
root `<svg style="--fg:...">`, referenced by `var()` from presentation
attributes, is how generated chart/diagram exports do the same. Without
handling either, every such file fails to render — which is not a corner
case; it was every SVG in one real drive.

`svgThumbnail` (`generators.go`) substitutes `currentColor` before parsing,
and `resolveCSSColors` (`svg_colors.go`) does the same for `var()` — but it
is deliberately not a CSS engine: no cascade, no specificity, no computed
color mixing. It resolves `var(--name)` only against literal declarations on
the _root_ element's own `style` attribute (a few passes let a short chain of
var-referencing-var resolve), honors a `var()`'s own CSS fallback argument
when the name itself is undefined, and flattens anything still
standing — an undefined `var()` with no fallback, or a bare `color-mix()` —
to a fixed fallback color. The result renders in the wrong color rather than
not rendering at all.

### Office

Office documents convert to PDF through OnlyOffice and then take the PDF path,
so the rasterizer is shared rather than duplicated.

The awkward part is structural: OnlyOffice's conversion API **fetches the
document by URL** rather than accepting an upload, and `cmd/worker` has no
listener to serve one from. So the worker signs a short-lived, view-only WOPI
token and points OnlyOffice at the API server's existing route:

```
worker ──POST /converter {url: <api>/wopi/files/<id>/contents?access_token=…}──> OnlyOffice
                                                                                     │
worker <──────────────────── {endConvert: true, fileUrl: …pdf} ──────────────────────┘
   └── GET fileUrl → PDF bytes → pdftoppm → imageThumbnail
```

Three consequences worth knowing:

- **`PP_SESSION_KEY` must be identical in the server and the worker.** The
  worker signs the token; the _server_ verifies it. When the variable is unset
  each process generates its own key at startup, so every conversion would fail
  with an opaque "invalid or expired WOPI token" after five retries. The worker
  detects this (`config.SecretKeyShared`) and **declines to register the office
  renderer**, leaving those jobs pending and logging what to fix.
- **Two URLs, two directions.** `PP_ONLYOFFICE_INTERNAL_URL` is OnlyOffice as
  the worker reaches it (container network, not the browser-facing
  `PP_ONLYOFFICE_URL`). `PP_WOPI_SERVER_URL` is the API server as _OnlyOffice_
  reaches it — the same address the WOPI editor already needs.
- **The cache key is the blob hash.** Content-addressed, so the same document
  converts once ever, however many times it is enqueued.

The token is view-only (rank 1) with a 5-minute TTL, signed for the nil-UUID
system subject — nothing here acts on a user's behalf, and
`handleWopiGetFile` authorizes on the token alone.

**The conversion request is signed.** Both compose stacks run the Document
Server with `JWT_ENABLED=true`, because it has no other access control and its
conversion API takes a URL to fetch — unsigned, anything that reaches its port
can use it to probe the network. The worker signs with
`PP_ONLYOFFICE_JWT_SECRET`, which must equal the Document Server's
`JWT_SECRET`; a mismatch surfaces as error `-8` in `jobs.last_error`, which
`convertError` spells out. See
[office-integration.md](office-integration.md).

## Caching

```
GET /api/v1/nodes/{id}/preview?kind=thumbnail&v={node.version}

ETag: "<blob_hash>"
Cache-Control: private, max-age=31536000, immutable
If-None-Match match → 304
```

Two details are load-bearing:

- **`private`** — the response is only reached through `requireNodeCaps`, so
  a shared proxy caching it would serve one company's thumbnail to a member of
  another, straight past the grants in [permissions.md](permissions.md).
- **`&v=`** — `immutable` tells the browser never to revalidate, so the URL has
  to change when the file does. `previewUrl` in `apps/web/src/lib/api/utils.ts`
  appends `node.version`; without it an edited file's old thumbnail stays
  pinned in every viewer's cache for a year.

The ETag is the blob hash: previews are content-addressed, so an exact
validator costs nothing.

## Why a preview can be missing

A 404 with `{"title":"Not Found","detail":"preview not ready"}` means no
`previews` row for that version and kind:

- **The MIME isn't mapped** — no job was ever enqueued.
- **No worker can render it.** `render:pdf` and `render:video` need
  `cmd/worker`; without it those jobs stay pending. This is a supported
  configuration, not a fault.
- **The job hasn't run yet.** Workers tick every 3s, so a fresh upload 404s
  briefly.
- **The version predates the pipeline.** `Enqueue` only runs on upload; run
  `worker --backfill` (below).
- **The job failed 5×** (`maxAttempts`) — check `jobs.last_error`.

The web client treats a 404 as a state, not an error. `PreviewContent`
(`apps/web/src/components/preview-content.tsx`) fetches the blob itself and
renders a message; it must not point an `<iframe>` straight at the endpoint, or
the browser paints the raw problem+json body.

## Backfill

`Enqueue` only fires on upload, so deploying the worker gives thumbnails to new
files only — every PDF already in the drive stays blank, which reads as a
broken feature rather than a missing sweep.

```
docker compose -f deploy/compose/compose.prod.yml run --rm worker --backfill
```

`preview.Backfill` scans current versions of live files that have no preview
and no in-flight job, keyset-paginated, 200 at a time with a 250ms pause. Only
_current_ versions are considered — `handlePreview` serves
`nodes.current_version_id`, so rendering superseded versions would burn CPU on
blobs no endpoint can reach. Safe to re-run.

Text extraction has its own sweep, `preview.BackfillText`, which `--backfill`
also runs — and which `cmd/server` runs by itself at every startup, because its
extractors live there: a drive upgraded to content search becomes searchable
without an operator step. It skips files already indexed or queued.

Known gap: there is no priority lane, so on a large install fresh uploads queue
behind the backlog. The fix is a `run_at` offset on backfilled jobs.

## Serving the original file

`GET /api/v1/nodes/{id}/content` streams the file's own bytes with
`Content-Disposition: inline`, so the browser renders them in place. It is the
same read path as `/download` (`serveBlob` in
`apps/server/internal/httpapi/drive.go`) with three differences:

- **Inline, not attachment** — no save dialog, so a PDF lands in the browser's
  built-in viewer and a video plays in `<video>`.
- **Allowlisted types only.** Blobs are served from the app's own origin, so
  `inlineMimes` permits PDF, the raster image types, video, audio and
  `text/plain`; everything else (HTML, SVG, scripts) is downgraded to
  `attachment`. `X-Content-Type-Options: nosniff` is always set. The web mirror
  is `INLINE_MIMES`/`canOpenInTab` in `apps/web/src/lib/api/utils.ts`, covered
  by `apps/web/tests/inline-content.test.ts`.
- **Range requests** — served through `http.ServeContent`, which PDF viewers
  and `<video>` need in order to seek.

`PreviewBody` dispatches on **MIME**, not on preview kind. Since images, PDFs
and video all store one `thumbnail`, the preview kind can no longer distinguish
`<img>` from `<object>` from `<video>` — and "what can the browser render
inline" was always a property of the MIME type anyway. Only `text` reads the
generated blob outright; a 4KB head is all the viewer shows.

Images are the case worth spelling out: `ImagePreview` points at `/content` and
falls back to the 256px thumbnail only if those bytes fail to load.

SVG is the wrinkle in that fallback: `PreviewBody` dispatches it into
`ImagePreview` too (its MIME starts with `image/`), so the full-size viewer
renders it via `<img src=".../content">` even though `inlineMimes` downgrades
that same request to `attachment`. That is not a bug — `<img>` (and `<video>`,
`<object>`) render a response's bytes regardless of `Content-Disposition`;
only top-level navigation and `/download` respect it, and neither happens
here. The grid and details panel instead show the real `/preview` thumbnail
`SVGRenderer` rasterized, same as any other image type.

## Still to come

All three phases are delivered:

| Phase | Scope                                                                     | Status |
| ----- | ------------------------------------------------------------------------- | ------ |
| 1     | Contract split, optional worker, PDF + video renderers, caching, backfill | done   |
| 2     | `S3Store` + `PP_BLOB_BACKEND` (see [storage.md](storage.md))              | done   |
| 3     | Office thumbnails via OnlyOffice `POST /converter` → PDF → rasterizer     | done   |

Still open:

- **Backfill has no priority lane** — on a large install, fresh uploads queue
  behind the sweep. Fix is a `run_at` offset on backfilled jobs.
- **`previews` rows are never reaped when a version is superseded.** Blob GC
  collects the bytes once nothing references them, but the row itself lingers.
