# Backend Conventions (Go)

Stack: Go, chi (HTTP router), sqlc + pgx (Postgres), go-oidc (OIDC), blake3 (content hashing). No ORM, no heavy framework — go-zero/kratos/gorm were explicitly rejected (too much magic, poor fit for file-tree/permission queries).

## Module Boundaries

- Each `internal/<module>` exposes an interface; other modules depend on the interface, never on concrete types or sibling internals.
- Cross-module imports of unexported packages are forbidden — this discipline is what keeps the monolith splittable later.

## Data Access

- All SQL lives in sqlc query files; generated code lands in `internal/store`. Hand-written SQL in handlers is prohibited.
- One transaction per write use case (e.g., blob commit + version insert). Long work (hashing, streaming) happens outside transactions.
- **Schema changes are numbered files in `internal/store/migrations`.** `store.Migrate` applies them in filename order, one transaction per file, and records the filename in `schema_migrations`; sqlc reads the same directory as its schema, so a migration is also what regenerates `internal/store`.
- **The baseline is frozen.** The development-phase increments were squashed into one file, `0001_init.sql`, so the schema reads as a single document rather than fifteen deltas. That was acceptable exactly once, pre-production, with no deployed database to preserve — it is not a tool that comes out again. Every change after it is a new numbered file on top of it, never an edit to the baseline: `Migrate` skips a version already present in `schema_migrations`, so an edit is a no-op on every machine that has already applied it and the two schemas diverge with nothing to say so.
- **Invariants that live in SQL are tested against a real Postgres.** A recursive CTE or a CHECK constraint cannot be asserted against a fake, so those tests read `PP_TEST_DATABASE_URL`, call `store.Migrate` on it, and `t.Skip` when the variable is empty — `go test ./...` stays green without a database, and a missing database skips loudly instead of quietly proving nothing. Run them with `PP_TEST_DATABASE_URL=postgres://... go test ./internal/spaces/`. **Packages must run one at a time (`go test -p 1 ./...`)**: the packages share one database and `internal/preview/jobs_db_test.go` empties the `jobs` table, which deletes a pending job another package's test is waiting on. `internal/spaces/permissions_db_test.go` and `internal/preview/jobs_db_test.go` are the pattern to copy.

## HTTP API

- Routes in `internal/httpapi` only; business logic stays in modules.
- **Authentication is a middleware on the `/api/v1` group, never a line in a handler.** `r.Route("/api/v1", …)` calls `r.Use(s.authenticated)` and everything registered under it is authenticated before it runs. A handler takes the caller with `caller(r)` — never nil inside the group — or `callerSession(r)` when it also needs the session id; it does not re-check, re-resolve or defend itself. This replaced roughly thirty hand-written guards in front of seventy-odd handlers, where a new route was public until somebody noticed the missing line. A route that genuinely must be public is registered **outside** the group, next to `/healthz`, `/auth/*`, `/wopi/*` and `/s/*` — it is a deliberate decision made at the router, not an omission inside a handler. `TestEveryAPIRouteIsAuthenticated` (`internal/httpapi/routes_test.go`) walks the router and fails if an `/api/v1` route is not wrapped, so the rule is enforced rather than remembered. Calling `caller(r)` outside the group panics: it is a wiring bug, and failing loudly beats serving somebody else's data.
- **The same-origin check is a middleware too, and handlers never re-check it.** `r.Use(apiLimit.middleware, s.sameOrigin, s.authenticated)` on the `/api/v1` group is the whole of it, in that order: the limit before the check, the check before the session lookup, so a forged cross-site write costs no query and gets the real reason instead of a 401. A handler must not compare `Origin` itself — one middleware is auditable, seventy are not, and `TestEveryAPIMutationChecksOrigin` walks the router to prove the group covers every non-safe method. A route that genuinely cannot carry the check goes **outside** the group with a comment saying what authorizes it instead: `/wopi/*` is a signed access token and no `Origin` header at all, `/auth/callback` is the state and nonce in `pp_oidc_tx`. `TestWopiIsExemptFromOriginChecks` fails the build if somebody "fixes" the WOPI group by wrapping it, which would break every office save.
- **Reading is `CapView`; saving a copy is `CapDownload`.** A route that renders the file for a reader takes view — `/content` (the inline stream), `/preview?kind=text`, `/document`, `/attachments/{hash}`, comments, content search — and a route that hands a copy over to keep takes download: `/download`, the folder zip, `/export`, the copy source, `/collab`, and WOPI `GetFile` (rank ≥ 2). Choose the side deliberately when adding one, and set `X-Content-Type-Options: nosniff` plus an explicit `Content-Disposition` either way. Do not read this as a wall around the bytes: an inline render is the file, and a browser can save it, so a view route is not a claim that saving is impossible — it is the side that does not _offer_ a save. The design and its limits are stated in [permissions.md](../architecture/permissions.md).
- **Never read `r.RemoteAddr` directly. Use `audit.ClientIP(r)`.** It returns the address a _configured trusted proxy_ vouched for through `X-Forwarded-For`, and the TCP peer otherwise. Reading the headers yourself — or reinstating chi's `RealIP`, which is deprecated for believing them from anyone — makes the caller's address the caller's choice, and every control keyed on it decorative: the rate-limit bucket becomes resettable and the address on a session row becomes whatever an attacker wants its owner to see. Trust comes only from `PP_TRUSTED_PROXIES`, installed as `middleware.ClientIPFromXFF` in `Server.Router` and nowhere else.
- **The agent surface (M41) is `/mcp/`, outside `/api/v1`, and its tools never call a module.** `/mcp/` is bearer-only — authenticated by `mcpBearer` from a Logto access token for the `/mcp/` resource or a personal access token (`pph_…`), never the session cookie — which is why it is exempt from `sameOrigin` like `/wopi/` and `/hooks/`. Tools live in `internal/agenttools` and reach the server only through `agenttools.API`, which `agentAPI` (`httpapi/agent.go`) dispatches in process through the same `apiRoutes` table the network mounts, with the caller's session in the context: an agent runs exactly the web client's handlers, capability checks, audit rows and notifications included. A new tool therefore adds calls to existing routes, never a handler or a module call of its own — that would be a second place to enforce permissions and the one that drifts. A route only agents need goes under `if agent` in `apiRoutes`, with a comment giving the reason it must not be on the network; a route an agent must never reach — anything that manages credentials, like sessions and personal tokens — goes under `if !agent`. See [agent-access.md](../architecture/agent-access.md).
- **File content enters through bounded write routes.** Tus uploads declare `Upload-Length`, checked against the upload cap and pending quota before bytes are accepted; `uploads.Service.Append` bounds each chunk by the remaining length. The agent-only exceptions are text replacement (1 MiB decoded UTF-8), structured Office output (8 MiB, produced only by the isolated runner) and site-file publishing (bounded aggregate bytes/files). Every path enforces capabilities, quota and editor locks; text/Office replacement uses conditional filesystem commits against the read ETag. Markdown additionally uses `collab.Reseed` so unsaved room edits flush before comparison. Never add an unbounded replacement route or bypass commit-time checks. `fs.EnsureRoom` enforces quota on measured bytes. Outside tus, `ParseMultipartForm`'s argument is a memory spill threshold, not a body ceiling: use `parseUpload` for multipart and `limitBody` for raw streams. Fixed smaller limits are named constants shared with their consumers.
- **A new security-relevant state change gets an `audit.Event`.** Anything that changes who can reach what: authentication and its failures, session revocation, admin or suspension changes, grants, share revocation, membership. Record it with `s.audit.Record(r, audit.Event{…})`, adding a `Kind` constant to `internal/audit` — `Kind` is `TEXT` in the column, so a new verb is a code change rather than a migration. Put identifiers in `Detail`, not in the message. **`internal/activity` is not the place for it:** `activity_events.space_id` and `.actor_id` are both `NOT NULL`, which an authentication event cannot satisfy — it belongs to no space, and the interesting one, a failed sign-in, has no user to name — and the two tables have different readers, different retention and different consequences when a row is missing. One is a feed of work for colleagues; the other is the record somebody reads during an incident. An event that has both natures, like revoking a share, writes to both.
- **Nothing signs with `cfg.SecretKey`.** It is a master secret; every signer takes a purpose-bound subkey from `cfg.DeriveKey(purpose)` (HKDF-SHA256), with the purpose string a constant owned by the module that mints the token — `wopi.KeyPurpose` is the pattern. Two token classes signed with the master directly is what made the session cookie and the WOPI token inseparable: one leak compromised both and neither could be rotated alone. A new signer adds a purpose; it never reaches for the master, and it never reuses another module's purpose string.
- Errors are returned as RFC 7807 `problem+json`. Each module owns its error values and the edge maps them: `nodeError` (`fs.ErrNotFound` → 404, `fs.ErrNameConflict` / `fs.ErrCycle` → 409, `fs.ErrQuotaExceeded` → 413, `fs.ErrNotFile` / `fs.ErrNotDir` → 400), `spaceError` (`spaces.ErrNotMember` / `spaces.ErrForbidden` → 403, `spaces.ErrLastAdmin` → 409), `wopiLockError` (`wopi.ErrLockConflict` → 409 plus the current `X-WOPI-Lock` header, `wopi.ErrNotLocked` → 404). `httpapi.Error` is the fallback: it answers 500 with an empty detail and logs the cause, so an unmapped error cannot leak internals to a caller. A status that means something else in the protocol being spoken is not reused: `lockedByEditor` takes the status to answer, 409 on restore-version but **423** on the tus upload routes, where 409 is the offset-mismatch answer a client resumes from.
- The FE↔BE contract is currently hand-typed TS interfaces in `apps/web/src/lib/api/types.ts`; `openapi.yaml` codegen (oapi-codegen + TS client) is deferred until the API surface justifies it. Keep interfaces in sync with the Go handlers.

## Naming & Style

- Standard Go conventions (effective go / gofmt). `log/slog` is the logger — one logging library per binary, or logs stop being filterable as a set. Log with a module field.
- With a `ctx` or `r *http.Request` in scope, log through the `…Context` variant (`slog.WarnContext(ctx, …)`): `internal/obs`'s handler reads chi's request id from that context, and a record without it cannot be joined to its access log line or `security_events` row. Configure logging only through `obs.InitLogging`; never `slog.SetDefault` elsewhere.
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

Preview renderers shell out to `pdftoppm` and `ffmpeg` over user-supplied files.
Preview callers go through `internal/preview/exec.go`:

- `exec.CommandContext` with a timeout — never `exec.Command`.
- Bound the output **while reading**, not after the process exits; by then the
  memory is already allocated. Use `cappedBuffer` for stdout and `readCapped`
  for anything a binary wrote to disk.
- Spool input to a per-job temp dir and `defer os.RemoveAll`.
- These processes belong in `cmd/worker`, never in the API binary — they parse
  hostile bytes in memory-unsafe code, and the API process holds the master
  secret and the connection pool.

Structured Office processing is a separate, deliberate boundary:
`cmd/office-agent` / `internal/officeedit` implement a typed Unix-socket protocol
with pinned OfficeCLI, private request directories, deadlines, bounded I/O,
process-group cancellation and output validation. Only this credential-free,
network-isolated container starts the Office parser; the API uses its socket
client and never shells out. It does not share the worker's database, secrets
or blob mounts. Do not move its subprocess into an API handler or expose raw
OfficeCLI commands through the typed protocol.

## Process Bootstrap

`internal/bootstrap` builds config, the pgx pool, queries and the blob store.
The server and preview worker call it; do not construct their dependencies by
hand in `main`. The Office runner deliberately does **not** bootstrap the
application: it holds no database connection, blob store or master secret.

Exactly one process may run DDL. `cmd/server` migrates; everything else passes
`Migrate: false` and waits via `store.AwaitSchema`.

## Error Handling Invariants

- Blob write precedes metadata commit; never commit metadata for a blob that failed to persist.
- WOPI `PutFile` validates the expected `Version`; mismatch → 409, never blind overwrite.
