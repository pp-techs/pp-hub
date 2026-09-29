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
- **Invariants that live in SQL are tested against a real Postgres.** A recursive CTE or a CHECK constraint cannot be asserted against a fake, so those tests read `PP_TEST_DATABASE_URL`, call `store.Migrate` on it, and `t.Skip` when the variable is empty — `go test ./...` stays green without a database, and a missing database skips loudly instead of quietly proving nothing. Run them with `PP_TEST_DATABASE_URL=postgres://... go test ./internal/spaces/`. `internal/spaces/permissions_db_test.go` and `internal/preview/jobs_db_test.go` are the pattern to copy.

## HTTP API

- Routes in `internal/httpapi` only; business logic stays in modules.
- **Authentication is a middleware on the `/api/v1` group, never a line in a handler.** `r.Route("/api/v1", …)` calls `r.Use(s.authenticated)` and everything registered under it is authenticated before it runs. A handler takes the caller with `caller(r)` — never nil inside the group — or `callerSession(r)` when it also needs the session id; it does not re-check, re-resolve or defend itself. This replaced roughly thirty hand-written guards in front of seventy-odd handlers, where a new route was public until somebody noticed the missing line. A route that genuinely must be public is registered **outside** the group, next to `/healthz`, `/auth/*`, `/wopi/*` and `/s/*` — it is a deliberate decision made at the router, not an omission inside a handler. `TestEveryAPIRouteIsAuthenticated` (`internal/httpapi/routes_test.go`) walks the router and fails if an `/api/v1` route is not wrapped, so the rule is enforced rather than remembered. Calling `caller(r)` outside the group panics: it is a wiring bug, and failing loudly beats serving somebody else's data.
- **The same-origin check is a middleware too, and handlers never re-check it.** `r.Use(apiLimit.middleware, s.sameOrigin, s.authenticated)` on the `/api/v1` group is the whole of it, in that order: the limit before the check, the check before the session lookup, so a forged cross-site write costs no query and gets the real reason instead of a 401. A handler must not compare `Origin` itself — one middleware is auditable, seventy are not, and `TestEveryAPIMutationChecksOrigin` walks the router to prove the group covers every non-safe method. A route that genuinely cannot carry the check goes **outside** the group with a comment saying what authorizes it instead: `/wopi/*` is a signed access token and no `Origin` header at all, `/auth/callback` is the state and nonce in `pp_oidc_tx`. `TestWopiIsExemptFromOriginChecks` fails the build if somebody "fixes" the WOPI group by wrapping it, which would break every office save.
- **Reading is `CapView`; saving a copy is `CapDownload`.** A route that renders the file for a reader takes view — `/content` (the inline stream), `/preview?kind=text`, `/document`, `/attachments/{hash}`, comments, content search — and a route that hands a copy over to keep takes download: `/download`, the folder zip, `/export`, the copy source, `/collab`, and WOPI `GetFile` (rank ≥ 2). Choose the side deliberately when adding one, and set `X-Content-Type-Options: nosniff` plus an explicit `Content-Disposition` either way. Do not read this as a wall around the bytes: an inline render is the file, and a browser can save it, so a view route is not a claim that saving is impossible — it is the side that does not _offer_ a save. The design and its limits are stated in [permissions.md](../architecture/permissions.md).
- **Never read `r.RemoteAddr` directly. Use `audit.ClientIP(r)`.** It returns the address a _configured trusted proxy_ vouched for through `X-Forwarded-For`, and the TCP peer otherwise. Reading the headers yourself — or reinstating chi's `RealIP`, which is deprecated for believing them from anyone — makes the caller's address the caller's choice, and every control keyed on it decorative: the rate-limit bucket becomes resettable and the address on a session row becomes whatever an attacker wants its owner to see. Trust comes only from `PP_TRUSTED_PROXIES`, installed as `middleware.ClientIPFromXFF` in `Server.Router` and nowhere else.
- **File content enters through the upload routes; any other body that becomes stored bytes goes through `parseUpload` or `limitBody`.** Since M30 an upload is tus (`httpapi/uploads.go`): its size is bounded by the `Upload-Length` declared at creation, which is checked there against `s.uploadCap()` and the quota (with `PendingUploadBytes`) before a byte is accepted, and `uploads.Service.Append` copies each chunk through an `io.LimitReader` of what that length still allows — so no PATCH can write past it. Do not add a second route that accepts file content from a client: the cap, the quota, the editor lock and their re-check at commit live on these routes, and a second path is a second place to forget one. Quota is still enforced once the bytes have landed (`fs.EnsureRoom` inside `fs.putFile`): a declared length is a refusal courtesy, not a measurement. Outside tus, `ParseMultipartForm`'s argument is the in-memory spill threshold, not a ceiling, so a handler that parses a body without a ceiling lets one caller fill the disk: `parseUpload` for multipart (now only the avatar; it also turns the "body too large" parse error into a 413 with a sentence a user can act on), `limitBody` for a raw stream like WOPI `PutFile`. A handler with its own smaller, fixed ceiling declares it as a constant — the avatar's is `users.AvatarMaxBytes`, shared with the IdP picture fetch.
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

Renderers shell out to `pdftoppm` and `ffmpeg` over user-supplied files. All of
it goes through `internal/preview/exec.go`, and new callers must too:

- `exec.CommandContext` with a timeout — never `exec.Command`.
- Bound the output **while reading**, not after the process exits; by then the
  memory is already allocated. Use `cappedBuffer` for stdout and `readCapped`
  for anything a binary wrote to disk.
- Spool input to a per-job temp dir and `defer os.RemoveAll`.
- These processes belong in `cmd/worker`, never in the API binary — they parse
  hostile bytes in memory-unsafe code, and the API process holds the master
  secret and the connection pool.

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
