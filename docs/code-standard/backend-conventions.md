# Backend Conventions (Go)

Stack: Go, chi (HTTP router), sqlc + pgx (Postgres), go-oidc (OIDC), blake3 (content hashing). No ORM, no heavy framework — go-zero/kratos/gorm were explicitly rejected (too much magic, poor fit for file-tree/permission queries).

## Module Boundaries

- Each `internal/<module>` exposes an interface; other modules depend on the interface, never on concrete types or sibling internals.
- Cross-module imports of unexported packages are forbidden — this discipline is what keeps the monolith splittable later.

## Data Access

- All SQL lives in sqlc query files; generated code lands in `internal/store`. Hand-written SQL in handlers is prohibited.
- One transaction per write use case (e.g., blob commit + version insert). Long work (hashing, streaming) happens outside transactions.

## HTTP API

- Routes in `internal/httpapi` only; business logic stays in modules.
- Errors are returned as RFC 7807 `problem+json`. Typed errors: `ErrNotFound` (404), `ErrPermission` (403), `ErrQuota` (413), `ErrLocked` (409, WOPI lock conflict), `ErrVersionConflict` (409).
- The FE↔BE contract is currently hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`; `openapi.yaml` codegen (oapi-codegen + TS client) is deferred until the API surface justifies it. Keep interfaces in sync with the Go handlers.

## Naming & Style

- Standard Go conventions (effective go / gofmt). `zap` or `slog` for structured logging — pick once at M1, log with module field.
- Context is first param everywhere; no goroutine without an owner and a shutdown path.

## Background Jobs

- Jobs are rows in the `jobs` table; workers claim with `SELECT ... FOR UPDATE SKIP LOCKED`.
- Job handlers are idempotent; `attempts` drives bounded retry.
- **`jobs.kind` is a routing key, not a result type.** `ClaimJobs` filters on
  the kinds the calling process registered handlers for, which is what lets
  `cmd/worker` be optional: a process never claims work it cannot do, so that
  work stays `pending` instead of failing. Never widen a claim to all kinds.
- **The kinds a process claims are derived, never configured.** `NewWorker`
  computes them from the handlers passed in, and a handler whose external
  binary is missing returns `nil` from its constructor. A process therefore
  cannot advertise a capability it lacks.
- **`run_at` means "claimed at" on a `running` row.** `ClaimJobs` stamps it, and
  `ReapStuckJobs` uses it to return jobs abandoned by a dead worker. Anything
  that flips a row to `running` must stamp it too, or the reaper will take the
  job back mid-flight.
- Every job state must be reachable from every other: a row that only one
  process can move is a row that is lost when that process dies.

## Running External Binaries

Renderers shell out to `pdftoppm` and `ffmpeg` over user-supplied files. All of
it goes through `internal/preview/exec.go`, and new callers must too:

- `exec.CommandContext` with a timeout — never `exec.Command`.
- Bound the output **while reading**, not after the process exits; by then the
  memory is already allocated. Use `cappedBuffer` for stdout and `readCapped`
  for anything a binary wrote to disk.
- Spool input to a per-job temp dir and `defer os.RemoveAll`.
- These processes belong in `cmd/worker`, never in the API binary — they parse
  hostile bytes in memory-unsafe code, and the API process holds the session
  key and the connection pool.

## Process Bootstrap

`internal/bootstrap` builds config, the pgx pool, queries and the blob store.
Both entry points call it; do not construct these by hand in a `main`, or the
two drift and the gap shows up as a nil pointer in whichever process nobody
tested.

Exactly one process may run DDL. `cmd/server` migrates; everything else passes
`Migrate: false` and waits via `store.AwaitSchema`.

## Error Handling Invariants

- Blob write precedes metadata commit; never commit metadata for a blob that failed to persist.
- WOPI `PutFile` validates the expected `Version`; mismatch → 409, never blind overwrite.
