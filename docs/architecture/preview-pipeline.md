# Preview Pipeline

Background job workers generate a small preview blob per file version. The
browser fetches it from `GET /api/v1/nodes/{id}/preview?kind=…`.

Only `text` is read outright. PDFs and video have placeholder blobs, and the
`thumbnail` blob is too small for a full-size viewer, so all three render from
the original bytes at `GET /api/v1/nodes/{id}/content` instead — see
[Serving the original file](#serving-the-original-file). The thumbnail is still
what the details panel shows, and the fallback when an image's own bytes won't
load.

## Flow

1. `fs.PutFile` commits a new version and fires `fs.SetVersionHook`
   (wired in `apps/server/cmd/server/main.go`).
2. `preview.Worker.Enqueue` maps the version's MIME to a job kind via
   `kindForMime`. **An unmapped MIME enqueues nothing** — that version will
   never have a preview.
3. `preview.Worker.Run` ticks every 3s, claims jobs with
   `FOR UPDATE SKIP LOCKED`, generates the blob, and upserts a `previews` row
   keyed by `(node_version_id, kind)`.
4. `handlePreview` looks up that exact `(current_version_id, kind)` pair.

## The MIME → kind contract

`previews` rows are keyed by the kind the **worker** chose, and the endpoint
looks up the kind the **client** asked for. The two maps must agree exactly or
every request 404s with `{"title":"Not Found","detail":"preview not ready"}`.

| MIME                                                 | kind        | Generated blob                     |
| ---------------------------------------------------- | ----------- | ---------------------------------- |
| `application/pdf`                                    | `pdf`       | placeholder text (see Limitations) |
| `image/png`, `image/jpeg`, `image/gif`, `image/webp` | `thumbnail` | PNG, longest side ≤ 256px          |
| `video/mp4`, `video/webm`, `video/quicktime`         | `video`     | placeholder text (see Limitations) |
| `application/rtf`, `text/*`                          | `text`      | first 4KB, verbatim                |
| anything else                                        | —           | none; no job is enqueued           |

Both sides strip MIME parameters (`; charset=utf-8`) before matching.

Defined in two places that **must be kept in sync**:

- `kindForMime` — `apps/server/internal/preview/preview.go`
- `previewKind` — `apps/web/src/lib/api/utils.ts` (returns `null` for "no
  preview"; `canPreview` is derived from it)

Office formats without a preview (`.docx`, `.epub`, …) are not a gap — the UI
routes them to the OnlyOffice editor instead. See
[office-integration.md](office-integration.md). `.epub` in particular is a zip,
so a raw byte snippet would be garbage; it deliberately gets no preview.

## Why a preview can be missing

`preview not ready` means no `previews` row for that version and kind:

- **The MIME isn't mapped** — no job was ever enqueued.
- **The version predates the mapping.** `Enqueue` only runs on upload; there is
  no backfill, so widening `kindForMime` does not generate previews for files
  already in the drive.
- **The job hasn't run yet.** The worker ticks every 3s, so a fresh upload 404s
  briefly.
- **The job failed 5×** (`maxAttempts`) and was marked `failed` — check the
  `jobs` table's `last_error`.

The web client treats a 404 as a state, not an error: `PreviewContent`
(`apps/web/src/components/preview-content.tsx`) fetches the blob itself and
renders a message. It must not point an `<iframe>` straight at the endpoint —
the browser would paint the raw problem+json body.

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

The client uses it for the `pdf`, `video` and `thumbnail` kinds
(`PreviewContent`), for the "Open in new tab" row action, and for the Open
button in the preview modal. Only `text` comes from the generated preview blob
in the viewer — a 4KB head is all the viewer shows, so fetching the whole file
would buy nothing.

Images are the case worth spelling out: the generated thumbnail caps the
longest side at 256px, which is right for the details panel's 4:3 box but
renders as a stamp in a 90vw modal, so `ImagePreview` points at `/content` and
falls back to the thumbnail only if those bytes fail to load.

## Limitations

`pdf` and `video` kinds are placeholders, not rendered output — `pdfFirstPage`
returns a marker string and `videoProbe` a byte count
(`apps/server/internal/preview/generators.go`). Real rasterization needs
pdfium/pdftoppm and ffmpeg; both are post-M5, and until then nothing reads
those two blobs — the UI goes to `/content` instead. They still matter for the
MIME → kind contract above: `previewKind` must keep returning them so
`canPreview` stays true for PDFs and video. `handlePreview` serves every
non-thumbnail kind as `text/plain` today.

A PDF therefore has no thumbnail in the details panel (it falls back to the
file icon) and streams in full when previewed — fine for typical documents,
worth revisiting for very large ones once rasterization exists.
