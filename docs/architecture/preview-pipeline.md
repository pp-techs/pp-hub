# Preview Pipeline

Background job workers generate a small preview blob per file version. The
browser fetches it from `GET /api/v1/nodes/{id}/preview?kind=…`.

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

## Limitations

`pdf` and `video` kinds are placeholders, not rendered output — `pdfFirstPage`
returns a marker string and `videoProbe` a byte count
(`apps/server/internal/preview/generators.go`). Real rasterization needs
pdfium/pdftoppm and ffmpeg; both are post-M5. `handlePreview` serves every
non-thumbnail kind as `text/plain` today.
