# Production Readiness — Gap Analysis

M1–M19 delivered the product; M20 closed one of the items below. This document
is what stands between that product and an internal rollout, assessed against
the target deployment: **one company, 20–100 users, several departments,
self-hosted, no compliance auditor yet**.

It is a backlog, not a plan — the phasing at the end is a proposal, and nothing
here is scheduled. Most claims carry a `file:line` so a reader can check
whether it is still true before acting on it; where a line number would only
rot, the function or query is named instead.

The headline: the drive itself is finished. The permission model (M15) is
stricter by default than Google Drive's, WOPI co-editing is real, and
versioning, trash retention, blob GC, quota and search all work. What is
missing is the operational layer around it — the things that are invisible
until the day they are needed, and unrecoverable on that day.

## P0 — blocks an internal rollout

These are not features. Each one is either a way to lose data, a way to leak
it, or a step an administrator cannot perform at all. Item 3 has been closed
and is kept as a record of what was wrong and what replaced it; the other six
stand.

### 1. There is no backup

`deploy/` contains two compose files, two `.env` examples and
`seaweedfs/s3.json`. Grep for `pg_dump|backup|snapshot|cron` across `deploy/`
and `apps/server` returns one prose comment in `spaces/spaces.go:229`. There is
no Makefile, no CI, no volume snapshot script.

`pg_data` and `blob_data` have to be backed up **together and consistently**: a
database dump restored without its blobs is a directory listing of files that
no longer exist, and blobs restored without the database are content-addressed
garbage with no names. `docs/architecture/milestones.md:293` states the
intended procedure (`pg_dump` + rsync of `data/blobs`) as a sentence; nothing
implements it.

This is first because it is the only item on this list whose absence is
irreversible.

### 2. There is no mail transport

Grep `smtp|mailer|sendmail|gomail|sendgrid|notif` across `apps/server` and
`deploy/` returns zero functional hits. `go.mod` has no mail dependency; there
is no `net/smtp` import.

The consequences are already visible in the product:

- `handleCreateInvitation` mints a token, stores the hash, and returns
  `externalURL + "/invite/" + token` in the JSON response for the admin to copy
  and deliver by hand (`space_settings.go:248-272`). Onboarding forty people is
  forty copy-pastes into a chat client.
- Sharing notifies nobody. `handleCreateShare` records an activity row and
  returns the link to its creator (`share.go:119-152`); for `direct` and
  `group` grants the recipient is a known user with a known email address and
  still learns nothing.
- There is no in-app substitute: no `notifications` table in `0001_init.sql`,
  no `/notifications` route in `server.go:54-140`, no SSE or websocket.

At 20–100 users across departments, "share" that does not tell the recipient
anything is not a sharing feature.

### 3. The session cookie was a signature — closed in M20

Kept rather than deleted, because the blocker was real and because the shape
of the fix is why the rest of this item is now one bullet.

What it was: the cookie was `v1.<uuid>.<expiry>.<hmac>` — self-contained,
hard-coded to thirty days, with no `Secure` flag, no `sessions` table and
therefore nothing to revoke. A stolen cookie was good for a month. Over HTTPS
it worked, but the browser would also send it over plain HTTP, so one
accidental downgrade exposed a 30-day session to anyone on the path.
`HandleLogout` cleared the browser's copy and returned 204 while the IdP
session stood; the RP-initiated logout helper `EndSessionURL` had no caller in
either `apps/server` or `apps/web/src`. And one secret, `PP_SESSION_KEY`,
signed both session cookies and WOPI access tokens, so the only available
revocation — rotating it — signed out every user and invalidated every office
token at the same time.

What shipped:

- **`Secure` is derived from the scheme.** `auth.Service.cookie` builds every
  cookie in the package, `clearCookie` included, and sets `Secure` when
  `PP_EXTERNAL_URL` starts with `https://`, beside `HttpOnly` and
  `SameSite=Lax`. A deployment that is not https logs a warning at startup
  instead of being quietly insecure at the first request.
- **A session is a row** (`migrations/0002_sessions.sql`). The cookie carries
  an opaque 256-bit random token and the table stores only its SHA-256, so the
  table is not a source of usable cookies. `GetSessionByHash` resolves cookie
  → session + owner in one indexed join; `Authenticate` slides a 30-day idle
  deadline at most once an hour per session, under a 90-day absolute cap
  measured from `created_at`; `PurgeExpiredSessions` sweeps on a five-minute
  ticker in `cmd/server`.
- **Revocation exists, and it is a surface rather than an operator procedure.**
  `GET /api/v1/users/me/sessions`, `DELETE /api/v1/users/me/sessions/{id}` and
  `DELETE /api/v1/users/me/sessions` (all but the current one) back a Sessions
  tab on the account page. The user id is part of every delete predicate, so
  guessing somebody else's session id achieves nothing. A lost laptop is now a
  row someone can delete from their phone.
- **Logout reaches the IdP.** `HandleLogout` deletes the session row and
  answers `{"end_session_url": …}`; `signOut()` on the web side hands the
  browser to that URL, and falls back to clearing the query cache and routing
  to `/login` when the provider advertises no end-session endpoint. "Sign out"
  followed by "sign in" is no longer a silent SSO re-entry as the previous
  person on a shared machine.
- **One secret, purpose-bound subkeys.** `config.SecretKey` is a master secret
  nothing signs with directly; `config.DeriveKey` (HKDF-SHA256) issues a
  subkey per purpose and WOPI tokens take `wopi.KeyPurpose`. Sessions do not
  use it at all, so rotating `PP_SESSION_KEY` — still spelled that way
  deliberately, see [deployment.md](../deployment.md) — invalidates in-flight
  WOPI tokens and signs nobody out.
- **The login handshake is single-use.** State, nonce and the PKCE (S256)
  verifier live in one `pp_oidc_tx` cookie that `HandleCallback` clears before
  anything can return early; the `pp_oidc_state` / `pp_oidc_next` pair it
  replaces survived every failure path for their full ten minutes, leaving a
  replayable state value in the browser. The nonce is compared against the
  transaction in constant time, and a failed callback redirects to
  `/login?error=<code>` rather than answering a top-level navigation with
  plain-text `http.Error`.

Still open in this area, which is why the section survives:

- **No CSRF defence beyond `SameSite=Lax`.** No `Origin` or `Referer` check
  anywhere in `httpapi/`. `Lax` covers top-level navigations but the API is
  JSON over `fetch`, so this is thin rather than absent — and it is the one
  weakness here that M20 did not touch.

### 4. The Document Server boundary is unauthenticated

`compose.prod.yml` sets `JWT_ENABLED=false` **and**
`ALLOW_PRIVATE_IP_ADDRESS=true` / `ALLOW_META_IP_ADDRESS=true`.

Requests **to** the Document Server are therefore unauthenticated — anyone who
can reach it can drive it — and it will fetch private and cloud-metadata
addresses on request, which turns its conversion API into a probe of the
internal network. `office-integration.md` and `preview-pipeline.md` both
already record that JWT signing is not implemented; this notes that it is a
rollout blocker rather than a deferral. Switching it on is not a code change
alone: `ALLOW_PRIVATE_IP_ADDRESS=true` is load-bearing for the current compose
topology (`host.docker.internal`, the 172.16/12 range), so closing this
requires deciding the network layout first, and `JWT_ENABLED` is a breaking
change for every running deployment.

**Requests _from_ the Document Server were the other half, and that half is
closed.** A WOPI access token is a bearer credential OnlyOffice holds and
replays for its ten-hour life, and the permission rank inside it was the
caller's rank when the editor opened. Every handler trusted that rank, so a
grant could be revoked, a member removed from the space, or an account
suspended, and the editor kept saving for the rest of the day while every
`/api/v1` route refused the same person immediately — a WOPI token was the one
unrevocable credential left after M20 made sessions revocable.

`wopiClaims` now re-resolves on every WOPI request: it loads the node, checks
`users.suspended_at`, calls `share.EffectiveCapsForNode` and clamps the
token's rank to what the caller can do _now_ (`rankFromCaps`), refusing
outright at rank 0. The token is a ceiling, not an authority. Two consequences
are deliberate: a revoked _edit_ grant downgrades an open session to read-only
rather than killing it — `CheckFileInfo` reports the live `UserCanWrite`, so
the editor switches itself — and `PutFile` refuses a node in the trash, since
committing a version into a tombstone resurrects content nobody expects on
restore. A machine token (`wopi.SystemSubject`, the preview worker's
conversion fetch) has no subject to resolve and is authorized by the signing
key alone; it is read-only, single-node and short-lived, and it is now
refused at `CheckFileInfo` instead of answering 500 on a missing user row.

`internal/httpapi/wopi_db_test.go` holds the line against a real Postgres:
revocation, departure from the space, suspension, edit→view downgrade, and a
token minted for one file being replayed against another.

### 5. The first admin cannot be created

`users.is_admin` defaults to `false` (`0001_init.sql:44`). Its only writer is
`SetUserAdmin`, behind `requireAdmin` (`admin.go:88-96`). Grep for
`PP_ADMIN|ADMIN_EMAIL|bootstrapAdmin` across the repo and `deploy/` returns
nothing, and `bootstrap.go:44-79` does config, pool, migrate and blob only.

A fresh instance therefore has no administrator and no way to appoint one
except `UPDATE users SET is_admin = true` against Postgres by hand. An install
step that requires a psql session is not an install step.

### 6. Uploads are unbounded and nothing is rate limited

- `nginx.conf:48` sets `client_max_body_size 0` — explicitly uncapped at the
  edge.
- `handleUpload` and `handleUploadVersion` call `r.ParseMultipartForm(32<<20)`
  (`drive.go:543`, `drive.go:595`). That is an in-memory buffering threshold,
  not a size limit; the remainder spills to temp files. Grep for
  `MaxBytesReader|http.MaxBytes` across `apps/server` returns no matches.
- Quota is checked **after** the bytes land (`fs.go:130-137`, by design — a
  streamed multipart part does not declare its length), so an over-quota upload
  still consumes disk until the GC sweep reclaims it up to an hour later
  (`gc/gc.go:22-30`).
- The middleware chain is exactly `RequestID, RealIP, Logger, Recoverer`
  (`httpapi/server.go:43`). No rate limiter, no `middleware.Timeout`; `go.mod`
  has no rate-limit dependency. The anonymous share-link routes
  (`server.go:130-137`) are brute-forceable at wire speed.
- The server sets `ReadHeaderTimeout: 10s` and nothing else
  (`cmd/server/main.go:91`) — no write or idle timeout.

One user with a large disk can fill the volume for everyone else.

### 7. A REST upload silently overwrites an open editing session

`file_locks` exists (`0001_init.sql:286-291`) with a 30-minute TTL, and the
WOPI endpoints honour it (`wopi.go:134-176`). Nothing else does: grep of
`fs.go` finds no `GetLock` call, and there is no lock route under `/api/v1` in
`server.go:54-140`. There is no ETag or `If-Match` on the version-upload path
either.

So while A has a `.docx` open in OnlyOffice, B can `PUT /nodes/{id}/versions`
and A's work is overwritten on the next save. This is the one item on the P0
list that loses data during normal, well-intentioned use.

## P1 — the gap against Google Drive

Ordered by what a 20–100 person, multi-department deployment feels first.

### Search only matches file names

Migration `0007` added `pg_trgm` and a GIN index on `nodes.name`
(`milestones.md:78`). That is the whole of it: `GET /api/v1/search` ranks a
prefix match over a mid-string one and falls back to trigram similarity on the
name.

Google Drive indexes document contents and OCRs images. For a company document
store this is the difference between "find the contract" and "remember which
folder the contract is in" — and the second one stops working at about the
point a second department joins.

The pieces to build on already exist: the preview pipeline is a DB-backed job
queue with an optional worker (`preview-pipeline.md`), and office documents
already pass through OnlyOffice conversion, so a text-extraction job kind is an
addition to a working pattern rather than new infrastructure.

### `activity_events` is a feed, not an audit log

The table and service are real — 12 verbs, space-scoped and cross-space
readers (`activity.go:30-43`, `home.go:98-151`). As an audit trail it has four
holes:

| Missing                      | Evidence                                                                                                                         |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Reads and downloads          | No `viewed`/`downloaded` kind in `activity.go:30-43`; `handleDownload`, `handleContent` and `handleShareDownload` record nothing |
| Authentication events        | `auth.go` never imports `activity` — no login, logout or failed-login rows                                                       |
| Permission and admin changes | `handleUpsertGrant`/`handleRevokeGrant` (`groups.go`) and `handleAdminUpdateUser` (`admin.go:62-125`) write no activity row      |
| Request context              | No IP, user-agent or request id in the schema (`0001_init.sql:315-325`)                                                          |

Recording is also best-effort by design — errors are logged and swallowed
(`activity.go:85-88`), which is correct for a feed and wrong for an audit log.
There is no export endpoint, no instance-wide admin query (only
`ListSpaceActivity` / `ListMyActivity`), and no retention.

Concretely: "who downloaded the payroll folder?" has no answer today. At
20–100 people across departments that question gets asked.

### No comments

Drive's primary collaboration surface. No table, no endpoints, no UI — it has
been deferred at every milestone since M10 (`milestones.md:127`, `:150`) as "a
whole subsystem", which is accurate.

### Uploads are not resumable

The `uploads` table is in the schema (`0001_init.sql:229-242`) and **no Go code
references it** — it is dead schema. The transfer tray's pause stops the queue
rather than a transfer in flight (`milestones.md:150`). A 4 GB upload that
drops at 90% restarts at zero, and nothing sweeps abandoned rows because
nothing writes them.

### Storage accounting understates real usage

`node_versions` grows without bound (`0001_init.sql:207-220`); nothing prunes
old versions and there is no version-count cap. Quota, meanwhile, is computed
from the current version only (`fs.go:186-196`).

The two together mean the disk fills while the storage meter says there is room
— the numbers are not merely incomplete, they diverge over time. Either quota
counts every referenced blob, or versions get a retention policy; ideally both.

Also absent: per-user quota (no `users.quota_*` column, `0001_init.sql:33-50`),
file-count limits, and any egress accounting.

### No way to create a document

Grep for a blank-document template or a create-office-file path across `apps`
returns nothing. Every `.docx` has to be created somewhere else and uploaded
before it can be co-edited — which makes the editor a viewer with write access
rather than a place work starts.

### Identity is managed by hand

Users exist only after a successful OIDC login (`auth.Service.resolveUser`),
and only `email`, `email_verified` and `name` are read from the token. There
is no SCIM, no admin create-user, no pre-provisioning, and no group sync from
OIDC claims — so department structure that already exists in the IdP has to be
re-entered as pp-hub groups and kept in step manually.

Identity is keyed on `(oidc_iss, oidc_sub)`, but since M20 that key is no
longer the only way in: a miss falls back to the **verified** email and
re-points the existing account (`AdoptUserIdentity`), because `oidc_iss` is a
URL an operator can change and changing it used to make every returning user
look brand new — the insert then collided with the unique index on
`(tenant_id, email_hash)` and sign-in answered 500 for everybody. The fallback
is gated on `email_verified`; a genuine collision answers `email-taken` on the
sign-in page rather than a 500. This is a recovery path, not provisioning:
nothing here creates an account ahead of its owner's first login.

Deprovisioning is worse: `store/queries/users.sql` has no `DeleteUser`.
Suspension (`admin.go:73-125`) is the terminal state, and instance-level
ownership transfer does not exist — `RemoveMember`'s archive behaviour
(`spaces/spaces.go:228-236`) is per-space only.

### Smaller, but on the list

- **No antivirus or content scanning.** Uploads go from the multipart part
  straight into the CAS (`fs.go:127-129` → `blob/blob.go:42-71`) with no scan
  hook, no MIME sniffing (type is guessed from the extension, `fs.go:139-142`)
  and no extension blocklist.
- **No encryption at rest.** Local blobs are plaintext under `0755` dirs
  (`blob/blob.go:29-40`); the S3 backend sets no SSE headers; Postgres runs
  `sslmode=disable` inside the compose network (`compose.prod.yml:59,112`).
  Share and invite tokens _are_ stored hashed.
- **No security headers.** `apps/web/nginx.conf` has only `Cache-Control`
  `add_header`s (lines 121, 129) — no CSP, HSTS, X-Content-Type-Options or
  X-Frame-Options.
- **No metrics, tracing or error tracking.** Grep
  `prometheus|otel|opentelemetry|pprof|sentry` over `apps/server` and `deploy/`
  returns nothing. `log/slog` uses the default text handler with no level
  control (no `slog.New`/`SetDefault` anywhere), and the request id is not
  bridged into log records.
- **`/healthz` is a static 200** (`server.go:45-48`) — it touches neither
  Postgres nor the blob store, so it cannot fail when the thing it reports on
  has. There is no readiness split, and `cmd/worker` has no HTTP listener at
  all, hence no healthcheck in `compose.prod.yml:104-155`.
- **Avatar upload is a placeholder.** `users.go:78-103` accepts the file,
  discards the bytes, stores a placeholder URL, and carries a
  `// TODO: store to blob store`.
- **The UI is English-only.** No i18n library, no locale files, no string
  catalogue in `apps/web/src`.
- **No orphan-blob audit.** `gc.go:66-76` deliberately leaves a file behind
  when it crashes between deleting the row and the file, and defers to "the
  next full audit" — which does not exist.

## Out of scope, deliberately

Restating so these are not rediscovered as gaps:

- **Desktop/mobile sync clients** and offline access — a stated non-goal
  (`product-goals.md:31`), and the most common reason someone reaches for
  Drive instead. Worth revisiting only as its own project.
- **Multi-tenant SaaS operation** — the schema carries `tenant_id`, the product
  is single-tenant self-host (`product-goals.md:33`).
- **Mail, calendar, chat, contacts, federation.**
- **Domain-restricted share links** ("anyone at the company with the link").
  Link shares are public-only today; the per-space `allow_public_links` switch
  (`space_settings.go:100-112`) turns them off rather than narrowing them.
- **Legal hold / eDiscovery**, which a compliance obligation would pull in
  along with audit export and encryption at rest. Not assumed at this scale.

## Proposed phasing

Nothing below is scheduled; it is the order the dependencies suggest.

**Phase A — rollout blockers.** P0 items 1–7, of which **3 is done**: M20
brought the `sessions` table with it, so the one schema change this phase ever
needed is already applied. Of what remains, mostly infrastructure and
configuration rather than product: items 4, 5 and 6 are small and mechanical;
1 and 2 are the real work; 7 needs a decision about whether the REST path
refuses a locked node (simple, correct, occasionally annoying) or takes a lock
of its own.

**Phase B — the multi-department layer.** Audit-grade events (new kinds,
synchronous writes on the paths that matter, request context, an export), then
content-indexed search as a new preview-pipeline job kind, then IdP group sync.
These are what changes when the user count crosses a couple of departments, and
each one is additive to a pattern that already exists.

**Phase C — collaboration and storage honesty.** Comments, notifications
(which needs Phase A's mail transport), resumable upload against the dead
`uploads` table, version retention plus a quota that counts every referenced
blob, and document creation from templates.

**Continuous.** Observability (metrics, a real `/healthz`, structured logs with
the request id) is cheap at any point and makes every phase after it easier to
diagnose. It is listed last only because nothing depends on it.

## Verdict

A pilot with one department is reasonable once Phase A is done, provided
everyone understands that search finds names rather than contents, that shares
notify nobody, and that there are no comments. A company-wide rollout across
several departments needs Phase B as well — specifically the audit log and
content search, which are the two gaps that get noticed within the first week
rather than the first month.
