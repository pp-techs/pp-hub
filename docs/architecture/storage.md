# Storage: Content-Addressed Blob Store

## Blob Interface

```go
type Store interface {
    Put(ctx context.Context, r io.Reader) (hash string, size int64, err error) // streaming, blake3
    Get(ctx context.Context, hash string) (io.ReadCloser, error)
    Delete(ctx context.Context, hash string) error                              // GC only
}
```

## Local Implementation (MVP)

- Layout: `data/blobs/<2-hex-prefix>/<blake3-hash>`.
- Write path: stream to temp file → compute hash → atomic `rename`. Partial writes never appear under a content address.
- Consistency: blob is written before metadata commit; orphans (`ref_count=0` past grace period) are reaped by the GC job.

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
| Future move to S3/MinIO                      | `s3Store` implements the same `Store` interface; no caller changes       |

## WOPI Locking

`file_locks(node_id, lock_token, expires_at)`:

- `LOCK/UNLOCK/REFRESH_LOCK` via `POST /wopi/files/{id}` with `X-WOPI-Override`.
- Locks expire; an OnlyOffice crash leaks a lock only until `expires_at`; a GC pass clears expired rows.
- `PutFile` with a stale `Version` (concurrent direct upload) → 409; OnlyOffice re-merges.
