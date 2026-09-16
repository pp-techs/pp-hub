# Storage: Content-Addressed Blob Store

## Blob Interface

```go
type Store interface {
    Put(ctx context.Context, r io.Reader) (hash string, size int64, err error) // streaming, blake3
    Get(ctx context.Context, hash string) (io.ReadCloser, error)
    Delete(ctx context.Context, hash string) error                              // GC only
}
```

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
- Write path: stream to temp file → compute hash → atomic `rename`. Partial writes never appear under a content address.
- Consistency: blob is written before metadata commit; orphans (`ref_count=0` past grace period) are reaped by the GC job.

## S3 Implementation

Same `<2-hex>/<hash>` key layout as disk. That is not cosmetic: migrating an
existing install is `aws s3 sync data/blobs s3://bucket/` (or `mc mirror`) with
no key translation, and an operator reads either backend with one mental model.

`Put` spools to a temp file while hashing, then uploads. The spool is
unavoidable rather than lazy — the object key _is_ the content hash, so it is
unknown until the last byte is read, and S3 needs a seekable body to sign and
retry. It mirrors `LocalStore`, which also writes a temp file before renaming
into place, so both backends fail the same way when a disk fills mid-upload.
`HeadObject` before `PutObject` makes dedup skip the upload entirely.

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

| Risk                                         | Mitigation                                                               |
| -------------------------------------------- | ------------------------------------------------------------------------ |
| Crash between blob write and metadata commit | Orphan blob with `ref_count=0` → GC after grace period                   |
| `ref_count` drift                            | Periodic recount job recomputes from `node_versions`                     |
| Disk full during upload                      | Temp-file write fails before commit; quota check precedes version commit |
| Move to S3 object storage                    | `S3Store` implements the same `Store` interface; no caller changes       |

## WOPI Locking

`file_locks(node_id, lock_token, expires_at)`:

- `LOCK/UNLOCK/REFRESH_LOCK` via `POST /wopi/files/{id}` with `X-WOPI-Override`.
- Locks expire; an OnlyOffice crash leaks a lock only until `expires_at`; a GC pass clears expired rows.
- `PutFile` with a stale `Version` (concurrent direct upload) → 409; OnlyOffice re-merges.

## Garbage Collection

The write path is deliberately append-only: uploads add blobs and bump `ref_count`, deletes decrement it, and nothing removes bytes inline. That keeps the hot paths simple and makes a crash mid-delete safe — ref counts end up too high, never too low. The cost is that unreferenced blobs accumulate, which `internal/gc` sweeps every 15 minutes.

Two orderings carry the safety:

- **Only blobs unreferenced for over an hour are considered.** A blob is written and its `ref_count` incremented in two separate statements, so a brand-new blob sits at zero for a moment. The hour is the margin that stops an in-flight upload being collected out from under itself.
- **The row is deleted before the file.** A crash between them leaves a file with no row — a disk leak, findable by an audit. The reverse would leave a row promising bytes that are gone, which reads as corruption to everything downstream.

`DeleteBlob` re-checks `ref_count = 0` in its `WHERE`, so a blob that gained a reference between the sweep's `SELECT` and its `DELETE` survives.

Copying a file adds a reference rather than bytes, so duplicating a large file is a single row — and the blob stays alive while either copy points at it.
