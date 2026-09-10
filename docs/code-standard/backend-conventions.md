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

## Error Handling Invariants

- Blob write precedes metadata commit; never commit metadata for a blob that failed to persist.
- WOPI `PutFile` validates the expected `Version`; mismatch → 409, never blind overwrite.
