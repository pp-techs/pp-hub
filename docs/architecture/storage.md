# Storage: Content-Addressed Blob Store

## Blob Interface

```go
type Store interface {
    Put(ctx context.Context, r io.Reader) (hash string, size int64, err error) // streaming, blake3
    Ingest(ctx context.Context, path string) (hash string, size int64, err error) // a finished staged upload; takes the file over
    Get(ctx context.Context, hash string) (io.ReadCloser, error)
    Delete(ctx context.Context, hash string) error                              // GC only
    Check(ctx context.Context) error                                            // can it still accept writes (/readyz)
    Walk(ctx context.Context, fn func(hash string, size int64) error) error    // every stored blob (audit)
    Tidy(ctx context.Context, age time.Duration) (removed int, err error)      // abandoned temp files (audit)
}

type Claims interface {
    Claim(ctx context.Context, hash string, size int64) (replace bool, err error) // before the bytes are installed
    Healed(ctx context.Context, hash string) error
}
```

Every store is built with `Claims` (`store.Queries.BlobClaims()` in
`bootstrap`) and calls `Claim` once it knows the hash and **before** it
installs or deduplicates the bytes. That ordering is what keeps GC from
collecting a blob a write has just deduplicated against — see
[Claims and collection](#claims-and-collection). No caller has to order
"write bytes" against "record them": `Put` and `Ingest` return with the row
already there and pinned.

Two implementations: `LocalStore` (disk) and `S3Store`. `PP_BLOB_BACKEND`
picks one — `local` (default) or `s3` — and `bootstrap.openBlobStore` is the
only place that branches. Every caller (`fs`, `gc`, quotas, the preview worker)
holds the interface and cannot tell them apart.

Both are held to one `runStoreConformance` suite in
`internal/blob/conformance_test.go`, run against local always and against S3
when `PP_TEST_S3_ENDPOINT` points at one (SeaweedFS ships in both compose files; MinIO and AWS work too). A behavioural difference between
the backends would otherwise surface as a bug far away from the storage layer.

## Local Implementation

- Layout: `data/blobs/<2-hex-prefix>/<blake3-hash>`.
- Write path: stream to temp file (`tmp-*` at the root) → compute hash → **fsync** → claim → atomic `rename` → **fsync the shard directory**. Partial writes never appear under a content address, and the bytes are durable before the reference Postgres commits next: without the fsyncs a power cut could leave a committed version pointing at an empty or missing file. A new `<2-hex>` directory is made durable the same way (fsync of the root).
- Dedup is "a copy is at the address **and has the right length**". A same-named file of another length is a torn write; it is replaced, not reused, so later uploads of that content do not inherit the damage. A copy the scrub marked damaged (`Claim` answers `replace`) is overwritten however long it is, and the heal is recorded.
- `Ingest` (M30) hashes a finished resumable upload's staging file in one read pass and renames it to its content address — or deletes it when that content is already stored. The staging directory is `config.UploadDir()` = `<PP_BLOB_ROOT>/.uploads`, under the blob root on purpose, so the rename never crosses a filesystem and a multi-gigabyte upload is never copied at commit. If an operator mounts `.uploads` on another filesystem anyway, the rename fails with `EXDEV` and `Ingest` falls back to copying into a temp file in the store, fsyncing and renaming from there, instead of failing every commit.

## S3 Implementation

Same `<2-hex>/<hash>` key layout as disk. That is not cosmetic: migrating an
existing install is `aws s3 sync data/blobs s3://bucket/` (or `mc mirror`) with
no key translation, and an operator reads either backend with one mental model.

`Put` spools to a temp file while hashing, then uploads. The spool is
unavoidable rather than lazy — the object key _is_ the content hash, so it is
unknown until the last byte is read, and S3 needs a seekable body to sign and
retry. It mirrors `LocalStore`, which also writes a temp file before renaming
into place, so both backends fail the same way when a disk fills mid-upload.
`HeadObject` before `PutObject` makes dedup skip the upload entirely — when
the stored object has the right length and is not marked damaged; otherwise
it is overwritten, as on disk.
`Ingest` reads the staged file twice — once to learn the key, once to send it
— then removes it; there is no rename to make. The staging directory is
still `<PP_BLOB_ROOT>/.uploads` on local disk: it is the one path every
deployment already persists, so a restart does not throw away half an
upload, whichever backend holds the finished blobs.

`Get` maps `NoSuchKey` **and** a bare `404 NotFound` to `blob.ErrNotExist` —
implementations differ on which they return, and callers distinguish "gone"
from "broken" by that error alone. `Delete` treats missing as success, because
a GC sweep can race another delete and both have to finish.

Configuration is `PP_S3_BUCKET` / `PP_S3_ENDPOINT` / `PP_S3_REGION` /
`PP_S3_ACCESS_KEY_ID` / `PP_S3_SECRET_ACCESS_KEY` / `PP_S3_PATH_STYLE`.
Omitting the keys falls back to the AWS default credential chain, so a real AWS
deployment can use an instance or IRSA role instead of long-lived secrets.
Path-style addressing defaults **on**, since the self-host target is SeaweedFS.

`PP_BLOB_BACKEND=s3` with no bucket, or an unrecognised backend name, fails at
startup. Falling back to local disk would be the worst outcome: the server
would start, accept uploads and write them somewhere the operator believes is
empty, with no error until a restart loses them.

## Why Content-Addressed

- **Dedup for free** — identical content shares one blob across files/spaces.
- **Versioning nearly free** — a new version is a metadata row pointing at a (possibly pre-existing) hash.
- **Rename/copy = pure metadata** — no blob movement.
- **Sync-ready** — desktop clients later get delta sync by adding a rollsum chunk layer beneath `Put` (`chunkExists`/`chunkPut` endpoints) without changing the interface or callers.

## Hash Choice

blake3: much faster than SHA-256, collision resistance is sufficient for non-adversarial self-host dedup. Not a security boundary (share links use separate sha256 token hashing).

## Failure Modes & Mitigations

| Risk                                         | Mitigation                                                                                                          |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Crash between blob write and metadata commit | The claim already gave the blob a row; once the claim lapses GC collects it like any unreferenced blob              |
| Crash before the claim (bytes, no row)       | The daily audit adopts the file with its true reference count; GC then collects it if nothing references it         |
| `ref_count` drift                            | Impossible by construction (triggers); the daily audit recounts anyway and logs an error for any correction         |
| Power cut after commit                       | Blob fsynced before its reference commits; the rename made durable by fsync of the directory                        |
| Bit rot / a torn copy                        | The scrub re-hashes stored bytes and marks damage; the next write of that content replaces the copy                 |
| Abandoned store temp files                   | The daily audit removes `tmp-*` (local) / `pp-blob-*` (S3 spool) older than 24 h                                    |
| Disk full during upload                      | Temp/staging-file write fails before commit; the chunk's error surfaces and the offset stays where the disk left it |
| Upload interrupted mid-chunk                 | Bytes that arrived are fsynced and kept; the client resumes from the recorded offset (M30)                          |
| Abandoned resumable upload                   | Row expires after 24 h idle; `uploads.Service` sweeps rows, then files, every 15 min, plus orphaned staging files   |
| Move to S3 object storage                    | `S3Store` implements the same `Store` interface; no caller changes                                                  |

## WOPI Locking

`file_locks(node_id, lock_token, expires_at)`:

- `LOCK/UNLOCK/REFRESH_LOCK` via `POST /wopi/files/{id}` with `X-WOPI-Override`.
- Locks expire; an OnlyOffice crash leaks a lock only until `expires_at`; a GC pass clears expired rows.
- `PutFile` with a stale `Version` (concurrent direct upload) → 409; OnlyOffice re-merges.

## Garbage Collection

The write path is deliberately append-only: uploads add blobs, references come and go as rows, and nothing removes bytes inline. That keeps the hot paths simple. The cost is that unreferenced blobs accumulate, which `internal/gc` sweeps every 15 minutes.

### Reference counts are the database's

`blobs.ref_count` is the number of rows that reference the hash: `node_versions.blob_hash`, `previews.blob_hash` and `users.avatar_blob`. Triggers on those three tables (migration `0011`) move it in the same statement as the reference — including rows removed by `ON DELETE CASCADE`, which is how a purge, a pruned version or a superseded preview releases its blob. No Go code increments or decrements it. When a count reaches 0 the trigger stamps `released_at`.

Before `0011` the application moved the count by hand, in statements separate from the references. A failure between them left a count too low — the direction that deletes live data — and previews were never counted at all. The migration recomputed every count from the referencing rows.

### Claims and collection

A blob is **collectable** when `ref_count = 0`, it was released over an hour ago (`released_at`, so an old blob just let go waits as long as a new one), no write has it claimed (`pinned_until`), and — repeated as a guard in SQL — no version, preview or avatar row names it.

The race this closes: a file deleted long ago releases its blob; the same content is uploaded again; the store finds the old copy and deduplicates; GC collects that copy before the new version references it; the new file points at nothing. `ClaimBlob` inserts or re-pins the row (`pinned_until = now() + 6h`) _before_ the store looks for an existing copy, on its own connection so a sweep sees it at once. The sweep deletes each blob in its own transaction — `DeleteCollectableBlob` re-checks every condition under the row lock, then the bytes are removed, then it commits; a failure to remove the bytes rolls the row back. A claim waiting on that row lock therefore finds either a live row whose bytes are there, or no row and no bytes, in which case the write installs its own. `TestCollectSparesABlobAnUploadJustDeduplicatedAgainst` reproduces the race.

The claim also means a write refused after its bytes landed — over quota, a name conflict, a crash — leaves a row, not an orphan file: when the claim lapses, the sweep reclaims it.

The hour of `released_at` grace doubles as the window an external backup has to copy the blobs a database dump references: a blob referenced at dump time cannot be collected until an hour after it is released.

### Audit

Daily (first run 10 minutes after start), `gc.Worker.Audit`:

1. `Tidy` removes the store's abandoned temp files older than 24 h.
2. `RecountBlobRefs` sets every `ref_count` to the number of referencing rows and logs an error for each it had to correct — with triggers there should be none.
3. `Walk`s the store in batches and **adopts** every blob with no row (`AdoptBlob`), with its true reference count. It never deletes: unreferenced adopted bytes are collected by the sweep, under the same guards as everything else. This is what reclaims crash leftovers and the preview blobs written before previews were counted.

A row whose bytes are missing is the scrub's to find.

### Scrub

Hourly, `gc.Worker.Scrub` re-hashes referenced, unpinned blobs, least recently verified first, until it has read `PP_BLOB_SCRUB_BYTES_PER_DAY / 24` (default 100G a day — 5 TB in about 50 days; `0` disables it). Bytes that are missing or no longer hash to their name mark the row `damaged_at`, log an error and show in `pphub_blobs_damaged`. Nothing can repair bytes the store no longer holds, but the next upload of the same content overwrites the copy instead of deduplicating against it, and clears the mark. A read error that says nothing about the bytes is retried on a later pass, not recorded.

Copying a file adds a reference rather than bytes, so duplicating a large file is a single row — and the blob stays alive while either copy points at it.

**Version retention** (M30) is the other source of unreferenced blobs. After a new version is saved, `fs.pruneVersions` deletes versions beyond `version_retention_count` (default 100, `0` keeps all); the triggers release their blobs (and their previews'), and this sweep reclaims the bytes. Unbounded history was what let the disk fill under a meter that — until M30 counted every stored version — still showed room.
