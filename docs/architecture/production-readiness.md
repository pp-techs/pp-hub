# Production Readiness — Gap Analysis

M1–M19 delivered the product; M20 closed one of the items below, M21 closed
three more plus a set of things nobody had written down, and M22–M24 closed
the rest of the P0 register — backup and restore, an invitation mail
transport, and the REST/WOPI write collision — along with two items that
were never on it: the readiness split and the avatar-upload stub. **M33 then
un-closed half of §2**: membership moved to a Logto organization, the space
invitation it was built to deliver no longer exists, and `internal/mail` was
deleted with it. §2 below is left as it was assessed and closed; the
heading says what happened to it. This document is what stood between that
product and an internal rollout, assessed against the target deployment:
**one company, 20–100 users, several departments, self-hosted, no compliance
auditor yet**.

It is the evidence, not the plan. [milestones.md](milestones.md) holds the
plan: every remaining gap below is carried by a named milestone, **M25
through M31**, and each heading here says which. Nothing there is
scheduled — the numbers are an order the dependencies suggest, and the
phasing at the end explains that order. What stays here is the reason each
item is (or was) on the list and the `file:line` that lets a reader check
whether it is still true before acting on it; where a line number would only
rot, the function or query is named instead.

The headline: the drive itself is finished, and as of M22–M24, so is the
operational layer that makes it safe to put real work in it — a restorable
backup, a readiness probe that actually fails when a dependency has, and a
REST write that cannot silently discard an open editor's work. The permission
model (M15) is stricter by default than Google Drive's, WOPI co-editing is
real, and versioning, trash retention, blob GC, quota and search all work.
What remains missing is the layer above that: content search, richer audit,
directory sync, comments, and the honest storage accounting Phase B and C
carry.

## P0 — blocks an internal rollout

These are not features. Each one is either a way to lose data, a way to leak
it, or a step an administrator cannot perform at all. Every item on this
register is now closed and kept as a record of what was wrong and what
replaced it: 3, 4, 5, 6 and 8 in M20 and M21; 1, 2 and 7 in **M22**, **M23**
and **M24**, plus two items that had no milestone of their own — the
static-200 healthchecks and the avatar-upload stub — closed alongside them,
recorded under "Smaller, but on the list" below since neither was ever a P0
item.

### 1. There was no backup — closed in M22

What it was: `deploy/` held two compose files, two `.env` examples and
`seaweedfs/s3.json`. Grep for `pg_dump|backup|snapshot|cron` across `deploy/`
and `apps/server` returned one prose comment in `spaces/spaces.go:241`. There
was no Makefile, no CI, no volume snapshot script — and `pg_data` and
`blob_data` had to be captured **together and consistently**: a database dump
restored without its blobs is a directory listing of files that no longer
exist, and blobs restored without the database are content-addressed garbage
with no names.

What shipped: `deploy/backup/pp-backup.sh` and `deploy/backup/pp-restore.sh`,
two POSIX `sh` scripts (`set -eu`) driving `docker compose` against
`compose.prod.yml` — no volume is ever named directly, since the file carries
no `name:` and a real volume name depends on the compose project, so every
volume is reached through `docker compose run`/`exec` on the service that
mounts it.

- **Postgres is dumped before blobs are archived, never the other way.** The
  pair that must never invert is a blob referenced by a row the dump already
  captured but whose bytes are missing from the archive because the archive
  ran first; dumping first and archiving second guarantees that never
  happens. A blob written in between is harmless either way — at worst an
  extra unreferenced file a future GC sweep reclaims.
- **The blob backend is read from `.env.prod`, not assumed.** `local` tars
  `/data/blobs` out of the `server` service; the bundled SeaweedFS (`s3` with
  `PP_S3_ENDPOINT` naming `seaweedfs`) tars `/data` out of the `seaweedfs`
  service; an external S3 bucket exits 2 with the reason — its own
  versioning and replication is the backup — after the Postgres dump has
  already been written, so relying on external S3 still leaves the database
  covered.
- **The backup verifies itself before declaring success.** `pg_restore
--list` on the dump and `tar -tzf` on the archive both have to pass, or the
  script exits non-zero with the artifact named — a corrupt backup answering
  "done" is worse than no backup, because nobody checks it until the day it
  is needed.
- **The restore verifies the dump before touching anything.** `pg_restore
--list` runs on the archive first; only then are `web`, `server` and
  `worker` stopped, Postgres is restored (`--clean --if-exists --no-owner`,
  no `--exit-on-error` — the notices from `--clean` against a fresh schema
  are expected), blobs are extracted, and the stack comes back up with a
  60-second poll of `/readyz` (M22 predates it only in numbering; both landed
  together) before reporting success.
- **`.env.prod` is explicitly out of scope**, and the backup says so on every
  run: `POSTGRES_PASSWORD`, `PP_SESSION_KEY` and the OIDC secrets are the
  operator's to keep, because a restore without them comes up as a different
  instance.
- **A restore has actually been performed against a built `compose.prod.yml`
  stack**, not merely scripted: a drill user row and a marker blob were
  written, `pp-backup.sh` captured both, `DROP SCHEMA public CASCADE` and
  `rm -rf /data/blobs/*` destroyed them, and `pp-restore.sh` brought both
  back — the row, the blob's bytes, and `schema_migrations`' newest version
  all verified byte- and value-identical afterward. The one part of the
  script that did **not** pass in that drill was the final readiness poll,
  because the drill used an unreachable OIDC issuer and `cmd/server` blocks
  its listener on discovery before `/readyz` exists to answer — exactly the
  caveat this document's own verification steps name, and unrelated to
  whether the data came back.

Retention defaults to 14 days (`-k`), the output directory defaults to
`backups/` at the repo root (`-o`), and neither is installed as a cron job —
the suggested line is a comment in the script's header, for an operator to
add.

### 2. There was no mail transport — closed in M23, reopened by M33

> **The transport is gone.** M33 made space membership a projection of a Logto
> organization, so the space invitation — the only message this transport ever
> sent — no longer exists, and `internal/mail`, `config.validateSMTP` and the
> `PP_SMTP_*` variables were deleted with it. Invitation email is now Logto's,
> through an email connector and its `OrganizationInvitation` template
> ([logto-org-sync.md](logto-org-sync.md), [deployment.md](../deployment.md)).
> The assessment below is kept as it was written: it is why the item was
> closed, and the notification half it deferred to **M29** is now that
> milestone's whole problem, transport included.

What it was: grep `smtp|mailer|sendmail|gomail|sendgrid|notif` across
`apps/server` and `deploy/` returned zero functional hits, `go.mod` had no
mail dependency, and `handleCreateInvitation` minted a token and returned
`externalURL + "/invite/" + token` in the JSON response for the admin to
copy and deliver by hand — forty people onboarded was forty copy-pastes into
a chat client.

**Decision taken: SMTP for invitations only.** Sharing still notifies
nobody — `handleCreateShare` records an activity row and a `share_created`
audit row, then returns the link to its creator (`share.go:115-176`), and a
direct or group grant's recipient, a known user with a known verified
address, still learns nothing. There is still no in-app substitute: no
`notifications` table, no `/notifications` route, no SSE or websocket. That
half is **M29**'s, which needs a notification story rather than a transport;
M23 built the transport M29 will reuse.

What shipped: `internal/mail`, a small `net/smtp` wrapper
(`Settings`/`Sender`/`Send`) — dial, optional `STARTTLS` or implicit TLS,
optional `PLAIN` auth, a hand-built RFC 5322 message, every failure wrapped
with the stage it failed at (dial, starttls, auth, …) rather than a bare
`smtp: EOF`. `To`/`Subject` are rejected on any `\r`/`\n` — both reach the
raw header block verbatim, so an unescaped newline in either is a
header-injection attempt.

- **A half-configured transport fails startup, not a send.**
  `config.validateSMTP` requires `PP_SMTP_FROM` whenever `PP_SMTP_HOST` is
  set (every relay refuses a message with no sender), restricts
  `PP_SMTP_TLS` to `starttls`/`implicit`/`none`, and bounds the port —
  because the operator who set `PP_SMTP_HOST` believes mail works, and
  finding out otherwise at the first invitation is worse than finding out at
  startup. `Host == ""` is a supported, logged state, not an error.
- **The invitation handler degrades to exactly the pre-M23 behaviour.**
  `handleCreateInvitation` sends synchronously, bounded by a 15s context,
  after the invitation row is already committed; a send failure is logged
  and the response still carries the URL — the admin's fallback is the
  product's old and only behaviour, not a new failure mode. The response
  gains `mail_sent`, which the listing endpoint omits because it knows
  nothing about delivery; the web client branches its toast on it and keeps
  the clipboard copy in both paths.
- **Verified end to end against a real relay**, not just the transport's
  unit tests: `internal/mail/send_live_test.go` (skipped without
  `PP_TEST_SMTP_ADDR`) sent one message through a MailHog catcher run for
  this milestone, and the message arrived — `curl
localhost:8025/api/v2/messages` reported it.

What honestly remains: OAuth-only providers (Gmail without an app password,
Microsoft 365 modern auth) are not supported by `net/smtp` — a relay in
front of them is the answer, stated in [deployment.md](../deployment.md)
rather than discovered at the first bounce.

### 3. The session cookie was a signature — closed in M20 and M21

Kept rather than deleted, because the blocker was real and because the shape
of the fix is why the last gap in it was worth closing too.

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

**The last gap here — no CSRF defence beyond `SameSite=Lax` — closed in M21.**

What it was: no `Origin` or `Referer` comparison anywhere in `httpapi/`, so a
browser policy on one cookie attribute was the entire defence against a
cross-site write. `Lax` withholds the cookie from a cross-site `fetch`, which
is most of it, but it is one browser's policy rather than this server's
control, it does not cover a top-level form navigation, and a single control
is exactly the thing that should not be the only one.

What shipped: `Server.sameOrigin` (`httpapi/csrf.go`), a middleware on the
`/api/v1` group and on `/auth/logout`. It reduces `Origin` to
scheme-plus-host and compares that to `PP_EXTERNAL_URL`; with `Origin` absent
it falls back to the **origin** of `Referer` and never its path, which the
caller controls; with neither header present a cookie-authenticated write is
refused rather than guessed about. An unparseable `PP_EXTERNAL_URL` refuses
every mutation instead of allowing them, so a misconfiguration cannot become
a silent bypass. `GET`, `HEAD` and `OPTIONS` pass untouched — demanding an
Origin on them would break a plain `<a href>` download.

It runs **before** `authenticated`, deliberately: a forged cross-site write
should cost no session lookup, and the caller should be told the actual reason
rather than a 401 that sends them hunting for an expired login. Two route
groups are exempt and both are authorized by something other than a cookie —
`/wopi/*`, where the Document Server sends no Origin at all and presents a
signed access token, and `/auth/callback`, a top-level navigation from the IdP
defended by the state and nonce in `pp_oidc_tx`.

Both halves are structural rather than remembered:
`TestEveryAPIMutationChecksOrigin` walks the router and fails if any non-safe
`/api/v1` method is not wrapped; `TestWopiIsExemptFromOriginChecks` fails if
the WOPI group ever is, because that would break every office save. The unit
tests in `csrf_test.go` cover the host-prefix near-miss
(`https://drive.example.com.evil.example`), a scheme downgrade and a
different port, the Referer fallback, the both-headers-absent refusal and the
unparseable-URL fail-closed.

### 4. The Document Server was unauthenticated — closed

Both compose files used to set `JWT_ENABLED=false`, and prod also set
`ALLOW_META_IP_ADDRESS=true`. The Document Server has no access control of its
own, so that combination meant anything able to reach its port could drive its
conversion API — an API whose input is a URL to fetch — with permission to
reach private _and_ cloud-metadata addresses. Either alone is a weak posture;
together they were a rollout blocker.

Both halves are now closed, and they were different problems.

**Requests to the Document Server are signed.** `JWT_ENABLED=true` in both
stacks with a shared `PP_ONLYOFFICE_JWT_SECRET`, and
`preview.signOnlyOfficeJWT` signs every conversion request the worker makes —
body `token` claim and `Authorization: Bearer` header both, because the docs
disagree about which endpoints honour which. Unsigned and wrongly-signed
requests come back `-8` with no fetch attempted, verified against a live 9.4.0
with the metadata endpoint as the target URL. That is what makes the remaining
`ALLOW_PRIVATE_IP_ADDRESS=true` tolerable rather than an open door: it is
load-bearing for the topology — the WOPI host is at a 172.16/12 container
address — but it is no longer reachable by an unauthenticated caller.
`ALLOW_META_IP_ADDRESS` is gone from production entirely; the range it opens
contains `169.254.169.254` and nothing in that topology lives there. Only
development still sets it, because `host.docker.internal` resolves into it.

The old belief that `JWT_ENABLED=true` breaks WOPI was half of the truth: the
conversion call was unsigned, so it did break office thumbnails. WOPI itself
is authorized by the access token, not a JWT, and a full editor round trip —
`CheckFileInfo`, `LOCK`, `GetFile` — was verified working with JWT on.

**Requests from the Document Server carried frozen authorization, and no
longer do.** A WOPI access token is a bearer credential OnlyOffice holds and
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

### 5. The first admin cannot be created — closed in M21

What it was: `users.is_admin` defaults to `false`, and its only writer,
`SetUserAdmin`, sits behind `requireAdmin`. A grep for
`PP_ADMIN|ADMIN_EMAIL|bootstrapAdmin` across the repo and `deploy/` returned
nothing, and `internal/bootstrap` did config, pool, migrate and blob store and
nothing else. A fresh instance therefore had no administrator and no way to
appoint one except `UPDATE users SET is_admin = true` against Postgres by
hand. An install step that requires a psql session is not an install step.

What shipped: `PP_BOOTSTRAP_ADMIN_EMAIL`, read in `config.Load` (lowercased
and trimmed there, compared case-insensitively, because an IdP is free to
return a different case than the operator typed) and applied by
`auth.Service.promoteBootstrapAdmin` on the sign-in path. Three properties
are deliberate:

- **It re-promotes on every sign-in, not once.** A one-shot bootstrap solves
  the empty instance and nothing else; running every time also recovers an
  install whose last admin was demoted by mistake, which is the same dead end
  arriving later. The write is idempotent — the function returns early when
  the account is already an admin — so the ordinary case costs one
  comparison. The consequence, warned about in the log at the moment of
  promotion and stated in [deployment.md](../deployment.md), is that the named
  account cannot stay demoted while the variable is set; unset it once a
  second admin exists.
- **It requires a verified email claim.** The address is matched against a
  static configuration value, which makes the variable a standing
  authorization rule keyed on a claim a self-service IdP may let its users
  edit. Without the gate, anyone who could set their own address could take
  the instance. An unverified claim logs an error naming the fix and records a
  `login_failed` event with `code: bootstrap-admin-unverified` rather than
  failing silently.
- **Promotion is audited.** The row is an `admin_granted` event with
  `{"via": "PP_BOOTSTRAP_ADMIN_EMAIL"}`, so the one privilege escalation the
  configuration is allowed to perform is visible in the same log as every
  other one.

The related half is in §8: `TouchUserLogin` no longer overwrites
`users.email` from an unverified claim, which is what stops this variable
being reachable by a user who can edit their own address.

### 6. Uploads are unbounded and nothing is rate limited — closed in M21

What it was, in four facts that only added up when read together:

- `handleUpload` and `handleUploadVersion` called
  `r.ParseMultipartForm(32<<20)` and nothing else. That argument is the
  in-memory buffering threshold, not a size limit — the remainder spills to
  temp files — so a request body of any size was accepted and written to disk.
  A grep for `MaxBytesReader|http.MaxBytes` across `apps/server` returned no
  matches.
- Quota is checked **after** the bytes land (`fs.Service.checkQuota`, by
  design: a streamed multipart part does not declare a trustworthy length), so
  an over-quota upload still consumed disk until the GC sweep reclaimed it up
  to an hour later.
- A space's `quota_bytes` defaults to `0`, which means unlimited.
- The middleware chain was `RequestID, RealIP, Logger, Recoverer` — no rate
  limiter anywhere, and `go.mod` had no rate-limit dependency. The anonymous
  share-link routes were brute-forceable at wire speed against a namespace
  where a hit answers 200.

One authenticated user could therefore fill the volume for everyone else, and
one anonymous caller could guess share tokens as fast as the network allowed.

What shipped:

- **A body ceiling that stops the reader.** `PP_MAX_UPLOAD_BYTES` (default
  5 GiB, plain bytes or a `K`/`M`/`G`/`T` suffix, `0` disables) becomes
  `http.MaxBytesReader` in `limitBody`, so nothing past the ceiling is written
  anywhere. `parseUpload` wraps it for the two multipart handlers and
  translates the resulting parse failure into a 413 with a sentence a user can
  act on instead of "invalid multipart form"; WOPI `PutFile` streams a raw
  body and calls `limitBody` directly; avatars keep their own, much smaller
  `avatarMaxBytes`. `Content-Length` is used only as a courtesy early exit —
  it can lie or be absent under chunked encoding — and never as the
  enforcement. A bad value fails at startup rather than defaulting to
  unlimited: an operator who typed `5GB` meaning a ceiling should not be given
  "no ceiling" for the trouble.
- **Per-IP token buckets** (`httpapi/ratelimit.go`) on the three surfaces an
  anonymous caller reaches: `/auth/login`, `/auth/callback` and
  `/auth/logout` at 30/min with burst 10 (each costs an IdP round trip),
  `/s/{token}` at 60/min with burst 20 (a guessable secret), and `/api/v1` at
  1200/min with burst 200 (everything else). Buckets are per limiter as well
  as per IP, so exhausting one does not spend another; an empty bucket answers
  429 with `Retry-After`. Anything idle long enough to have refilled
  completely is evicted, which is what keeps a map keyed on an
  attacker-supplied value from being a memory leak.
- **A client address the caller cannot choose.** The limiter keys on
  `audit.ClientIP`, not `r.RemoteAddr`, and that is what makes it a limit at
  all — see §8, where chi's `RealIP` is retired.

What honestly remains:

- **The limiter is per process.** The buckets are a map in memory, so N
  `server` replicas allow N times the published rate. Redis would fix it and
  would be the first piece of infrastructure this deployment does not already
  need, for a control whose job is turning "millions of guesses an hour" into
  "hundreds". Stated in [deployment.md](../deployment.md) rather than hidden,
  because an operator who scales out should know the limit scaled with them.
- **There is still no per-user or per-space upload quota beyond
  `quota_bytes`.** The ceiling bounds one request; it does not bound a
  thousand of them, and a space with `quota_bytes = 0` is unmetered by
  definition. Per-user quota remains absent entirely — there is no
  `users.quota_*` column — and the accounting gap under P1 ("storage
  accounting understates real usage", **M30**) is unchanged.
- **The HTTP server still sets only `ReadHeaderTimeout: 10s`**
  (`cmd/server/main.go`). There is no write or idle timeout, and no
  `middleware.Timeout`; the long `proxy_*_timeout` values in `nginx.conf` are
  there because a multi-gigabyte upload and an OnlyOffice conversion both need
  them, which is also why a blanket handler timeout is not a one-line change.
  No milestone carries this one either — it belongs to whoever next touches
  the server's lifecycle.

### 7. A REST upload silently overwrote an open editing session — closed in M24

What it was: `file_locks` exists (`0001_init.sql:286-291`) with a 30-minute
TTL, and the WOPI endpoints honour it (`handleWopiPutFile` and
`handleWopiLockOps`). Nothing else did: `GetLock`'s only callers were all in
`internal/wopi`, there was no lock route under `/api/v1`, and there was no
ETag or `If-Match` on the version-upload path. So while A had a `.docx` open
in OnlyOffice, B's `PUT /nodes/{id}/versions` was accepted against no lock
at all, and A's work was gone on the next save with no conflict reported
anywhere — the one item on this list that lost data during ordinary,
well-intentioned use.

**Decision taken: refuse, do not take a lock.** A REST upload has no lock
lifecycle — no unlock call, no `REFRESH_LOCK` — so acquiring one here would
create locks nothing releases except the 30-minute sweep.

What shipped: `Server.lockedByEditor`, a shared check both write paths that
change which bytes are current now call — `handleUploadVersion`, before
`parseUpload` so a locked file is refused before a multi-gigabyte body is
read, and `handleRestoreVersion`, since restoring a version repoints
`current_version_id` just as directly. Either finds a live lock, it answers
`409 Conflict` with a sentence a client can show ("this file is open in the
editor; close it or wait for the lock to expire") rather than silently
discarding one side of the conflict. Not in scope, deliberately: trash,
move and rename of a locked node — none of them change file bytes, and
`handleWopiPutFile` already refuses to commit into a tombstone.

`TestRestVersionUploadRefusesALockedFile`
(`internal/httpapi/locks_db_test.go`) holds the line against a real
Postgres: a locked node refuses the upload and the version count stays put;
the same node unlocked accepts it and the count goes up by one.

### 8. What the sweeps found — closed in M21

Two read-only sweeps went through the server and the deployment looking for
the things this register had not thought to list. Everything below was real
and is fixed; it is recorded here because a list of closed findings is how the
next reader learns which mistakes this codebase is prone to. Ordered by what
an attacker could actually reach, not by how the fix reads.

1. **A "view only" share link served byte-identical originals.**
   `GET /s/{token}/content` did not consult the link's `can_download`, so a
   link created with downloads off handed over the complete file — and, one
   `?node=` at a time, every file in the shared subtree. No account, no
   session, nothing but the URL that had already been given out. It is the
   highest-reach finding of the set and the one whose fix is a single
   predicate: `serveShareFile` now requires the download flag on both the
   inline and the attachment path, and the comment that used to claim
   otherwise was simply wrong. There is no anonymous preview endpoint, so a
   no-download link is now names, sizes and dates and no file bytes at all —
   which is what "view only" should have meant, and what the share page now
   says in those words.
2. **A public link outlived its creator's access.** `resolveLinkNode`
   resolved the token hash and the expiry and stopped there, so revoking the
   grant that let somebody share a folder left every link they had minted
   working — anonymously, indefinitely. The codebase already held the opposite
   intent for the stronger action: `RemoveMember` runs
   `DeleteSharesByCreatorInSpace` so that a departing member's sharing
   decisions leave with them. Revoking one grant is the weaker action and left
   the bigger hole. A link now resolves only while
   `EffectiveCapsForNode(node, share.created_by)` still holds `CapShare`, and
   answers 404 — not 403 — once it does not. Re-resolving on read rather than
   cascading on revoke is the same choice made for WOPI tokens: one query
   covers every way access can disappear, including an ancestor being moved,
   instead of one cleanup path per way.
3. **A WOPI editor session was a download in disguise.** `GetFile` accepted
   rank 1, and `handleEditSession` accepted `CapView` on the reasoning that
   rank 1 produces a read-only session — true about writing, irrelevant to
   reading. A reader explicitly refused download could mint a view token, let
   the Document Server fetch the document, and keep the file, while
   `handleContent` refused the same person for the same bytes. `GetFile` now
   requires rank ≥ 2 for a user token and `handleEditSession` requires
   `CapDownload`, minting rank 2 even for a read-only session because rank 1
   would no longer be able to fetch its own document. Machine tokens
   (`wopi.SystemSubject`, the preview worker's conversion fetch) are
   unaffected: they have no subject to resolve a capability for and the
   signing key is their authorization. The cost is accepted and stated in
   [permissions.md](permissions.md) — a view-but-not-download reader cannot
   open an office document at all, only its thumbnail.
4. **`/preview?kind=text` was the source file.** The text "preview" is a
   verbatim 4 KiB prefix of the original, which for a note, a CSV, a config
   file or a private key is usually the whole document, and it was gated on
   view. `handlePreview` now demands `CapDownload` for `kind=text` and keeps
   `kind=thumbnail` on view — a 256px rasterization is genuinely derived, and
   it is what lets a no-download reader see _something_.
5. **An unverified email claim could rewrite `users.email`.** The adoption
   path in `resolveUser` had always required `email_verified`; the ordinary
   path had not, and rewrote the stored address from the claim on every
   sign-in. Since that column is what a direct share is granted to, what the
   member-add path resolves an address against, and what
   `PP_BOOTSTRAP_ADMIN_EMAIL` is compared against, an IdP that lets a user set
   their own address turned it into a way to be handed somebody else's access —
   or, with a bootstrap admin
   configured, to take the instance. The asymmetry was the bug; both paths now
   require verification, and admin promotion requires it separately.
6. **The client's IP address was the client's choice.** The chain began with
   chi's `RealIP`, which 5.3.2 deprecates for exactly this reason: it
   believed `True-Client-IP`, `X-Real-IP` and `X-Forwarded-For` from any
   source. So the address shown on a session row in somebody's Sessions tab
   was whatever an attacker wanted them to see, and any limit keyed on address
   would have been resettable at will — which is why this had to be fixed
   _before_ §6's limiter meant anything. It is replaced by
   `middleware.ClientIPFromXFF(PP_TRUSTED_PROXIES...)`, installed only when
   that variable names at least one CIDR, and read through `audit.ClientIP`,
   where "no vouched-for value" means "believe the TCP peer" rather than
   "believe the header". `config.Load` validates the CIDR list so a typo is a
   sentence at startup rather than a panic out of chi. nginx clears
   `True-Client-IP`, `CF-Connecting-IP` and `Forwarded` on the way through,
   since a header the proxy does not set is a header the client sets.
7. **Bearer credentials were written to the logs.** A share token _is_ the
   authorization for anonymous access to a file and it is a path segment, so
   chi's request logger put every live link on stdout — and from there into
   container logs, a log-search index or a support bundle. Only the token's
   SHA-256 is stored in the database; logging the plaintext beside it made
   that decision pointless. The WOPI `access_token` is the same problem in a
   query parameter. `redactingLogger` rewrites `r.RequestURI` — the raw string
   chi's Logger formats — to `/s/REDACTED/download` and
   `access_token=REDACTED`, and deliberately leaves `r.URL` alone: routing
   runs off `r.URL.Path`, and an earlier version that swapped the URL too made
   chi match the redacted path, so `{token}` reached the handler as the
   literal string "REDACTED" and every share link answered 404 while the log
   looked perfect. nginx does the same job with a `map` plus a custom
   `log_format`.
8. **`DELETE /nodes/{id}/shares/{shareID}` was not scoped by node.** The
   handler authorised `CapShare` on the node in the URL and then deleted by
   share id alone, so a caller who could share _any_ node could revoke a share
   belonging to a node they had no rights on. `DeleteShare` now takes
   `id = $1 AND node_id = $2` and a miss answers 404. Authorising on one row
   and mutating another is the shape of bug worth remembering, not the
   individual endpoint.
9. **A space admin nobody could see.** `spaces.SetAdmin` added the target to
   the space's `Admin` group without checking membership, which produced
   somebody who passes `IsSpaceAdmin` — and therefore every space-
   administration route, since those check admin status rather than membership
   — while having no `space_members` row, so the People page does not list
   them. Promotion now calls `RequireMember` first; the same check already
   guarded user-created groups, and the Admin group is the higher privilege of
   the two.
10. **The office MIME allowlist allowed everything.** `isOfficeMime` accepted
    `application/octet-stream`, which is precisely what `fs.PutFile` stores
    whenever Go's MIME table does not recognise an extension — so the
    allowlist read "any file with an unfamiliar name", and the editor handoff
    would hand an arbitrary blob to the Document Server. Removed.
11. **Secrets were landing in builder layers.** Docker reads
    `.dockerignore` from the root of the _build context_, and the server and
    worker images build with `context: ../../apps/server`, so the careful
    exclusions at the repo root governed only the web image. A bare
    `COPY . .` was pulling `apps/server/.env` — which holds
    `PP_OIDC_CLIENT_SECRET` and `PP_SESSION_KEY` — and `apps/server/data/blobs`,
    which on a developer machine is real user content, into the build. None of
    it reaches the final image, but it is retained in the local build cache and
    exported wholesale by any CI using `--cache-to` to a registry.
    `apps/server/.dockerignore` now exists and is kept in step with the root
    one.
12. **One flat compose network, and `/wopi/` published to the internet.**
    OnlyOffice is a third-party service whose only access control is a shared
    JWT secret and which runs with `ALLOW_PRIVATE_IP_ADDRESS=true` because its
    fetcher has to reach `server` at a private address; on a flat network that
    combination also let it open connections to `postgres:5432` and
    `seaweedfs:8333`. The network is now segmented `edge`/`data`/`office`,
    with `server` the only member of all three and OnlyOffice reaching
    `server:8080` and nothing else. Separately, nginx proxied `/wopi/`, which
    no shipped topology uses — `PP_WOPI_SERVER_URL` is `http://server:8080`,
    so the browser never calls that path — and publishing it turned the WOPI
    access token from a credential that mattered inside the network into an
    internet-wide bearer token, on the one route group with neither an origin
    check nor a rate limit. The `location` is gone. The `web` container also
    picked up `no-new-privileges`, `cap_drop: ALL` plus `NET_BIND_SERVICE`,
    and an nginx master running as `nginx` rather than root — it terminates
    every public request, so it gets the treatment the worker already had.
13. **Two byte-serving endpoints sent no `nosniff` and no disposition.**
    WOPI `GetFile` and the anonymous share-file path both derived
    `Content-Type` from the uploader's chosen extension and left a sniffing
    client nothing else to go on. Both now set
    `X-Content-Type-Options: nosniff` and an explicit `Content-Disposition`,
    which is what every other byte-serving path in the server already did.

## P1 — the gap against Google Drive

Ordered by what a 20–100 person, multi-department deployment feels first. Each
heading names the milestone that carries it; the phasing at the end places
that milestone in a phase.

### Search only matches file names — M27

`pg_trgm` and one GIN index on `nodes.name` — `nodes_name_trgm`, partial on
`deleted_at IS NULL` (`0001_init.sql:4` and `:200`; M7 added both, back when
the schema was a run of deltas rather than one squashed baseline). That is the
whole of it: `GET /api/v1/search` ranks a prefix match over a mid-string one
and falls back to trigram similarity on the name.

Google Drive indexes document contents and OCRs images. For a company document
store this is the difference between "find the contract" and "remember which
folder the contract is in" — and the second one stops working at about the
point a second department joins.

The pieces to build on already exist: the preview pipeline is a DB-backed job
queue with an optional worker (`preview-pipeline.md`), and office documents
already pass through OnlyOffice conversion, so a text-extraction job kind is an
addition to a working pattern rather than new infrastructure.

### `activity_events` is a feed, not an audit log — mostly closed in M21, the rest is M26

`activity_events` is still a feed, and that has stopped being a complaint:
M21 added a second table rather than more verbs to the first one. The reason
is structural, not stylistic. `activity_events.space_id` and `.actor_id` are
both `NOT NULL`, an authentication event belongs to no space, and the
interesting authentication event is the _failed_ one, where there may be no
local account to point an actor at. The two are also read by different people
for different reasons: one is a feed of work for colleagues, the other is a
record of access for whoever has to answer "who got in, from where, and when
did that permission appear". `security_events`
(`migrations/0003_security_events.sql`, service in `internal/audit`) is that
second table — no space, a nullable user, a free-text subject, and the request
context the first one has never carried. See
[data-model.md](data-model.md).

Where the four holes stand now:

| Hole                         | Status                                                                                                                                                                                          |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Reads and downloads          | **Still open, and deliberately so** — no `viewed`/`downloaded` kind anywhere; `handleDownload`, `handleContent` and `handleShareDownload` record nothing                                        |
| Authentication events        | **Closed** — `login`, `login_failed`, `logout`, `session_revoked`, `sessions_revoked`, `identity_adopted`, written by `internal/auth`                                                           |
| Permission and admin changes | **Closed** — `admin_granted`, `admin_revoked`, `account_suspended`, `account_unsuspended`, `grant_created`, `grant_revoked`, `share_created`, `share_revoked`, `member_added`, `member_removed` |
| Request context              | **Closed** — every row carries `ip` (through `audit.ClientIP`), a 400-byte-truncated `user_agent` and chi's `request_id`, so a row joins to the access-log line that produced it                |

`GET /api/v1/admin/security-events` pages the log newest first, admin-only and
deliberately **not** space-scoped: the whole value of a run of failed sign-ins
is seeing it across accounts and addresses at once. `?kind=` filters,
`?limit=`/`?offset=` page, and the actor's email is joined in so a row reads
as a person rather than a UUID — falling back to the recorded `subject` once
the account is gone. Retention is `PP_AUDIT_RETENTION_DAYS` (365), purged by
the same five-minute ticker in `cmd/server` that sweeps expired sessions and
WOPI locks: one row is written on every sign-in attempt, so "forever" is a
slow leak and, eventually, a console nobody can read.

Recording is still best-effort — the event describes something that already
happened, and failing a sign-in because its audit line could not be written
would trade the real outcome for the record of it — but a failure is logged at
**error** level rather than warn, and the insert runs on
`context.WithoutCancel` so a caller who disconnects mid-request cannot take
the row with them. Unlike an activity row, a gap here is a gap in an audit
trail.

**Read auditing was omitted on purpose, not overlooked.** A `downloaded` row
per byte-serving request is the highest-volume event in the system by an order
of magnitude — every thumbnail in a grid scroll, every range request a video
player makes, every WOPI `GetFile` behind an editor keystroke — and it would
have to be written on the hot path of the endpoints that matter most for
latency. Doing it properly means sampling or aggregation and a retention
policy of its own, which is a design decision rather than a missing call. So
"who downloaded the payroll folder?" still has no answer, and that decision is
what M26 has to make before it can write any code; what M21 bought is that
"who _gained access_ to the payroll folder, and when, and who granted it?"
does.

Still absent in this area, and the remainder of M26: an export endpoint (the
console reads JSON pages), and any alerting on the log — a hundred
`login_failed` rows from one address is visible to an admin who looks, and
nothing looks on its own.

### No comments — M29

Drive's primary collaboration surface. No table, no endpoints, no UI — it has
been deferred at every milestone since M10, whose own entry in
[milestones.md](milestones.md) says so and whose successor repeats it, as "a
whole subsystem", which is accurate.

### Uploads are not resumable — M30

The `uploads` table is in the schema (`0001_init.sql:229-242`) and **no Go code
references it** — it is dead schema. The transfer tray's pause stops the queue
rather than a transfer in flight, which M11's entry in
[milestones.md](milestones.md) already records as still-not-done. A 4 GB upload that
drops at 90% restarts at zero, and nothing sweeps abandoned rows because
nothing writes them.

### Storage accounting understates real usage — M30

`node_versions` grows without bound (`0001_init.sql:207-220`); nothing prunes
old versions and there is no version-count cap. Quota, meanwhile, is computed
from the current version only (`fs.Service.checkQuota`, `fs.go:175-202`: it
sums space usage and then subtracts the outgoing current version's size, so
every superseded version is invisible to the meter that is supposed to bound
the disk).

The two together mean the disk fills while the storage meter says there is room
— the numbers are not merely incomplete, they diverge over time. Either quota
counts every referenced blob, or versions get a retention policy; ideally both.

Also absent: per-user quota (no `users.quota_*` column, `0001_init.sql:33-50`),
file-count limits, and any egress accounting.

### No way to create a document — M31

Grep for a blank-document template or a create-office-file path across `apps`
returns nothing. Every `.docx` has to be created somewhere else and uploaded
before it can be co-edited — which makes the editor a viewer with write access
rather than a place work starts.

### Identity is managed by hand — M28

**M33 delivered the membership half of this.** Space membership and admin
status now arrive from a Logto organization, and reconciliation pre-creates
`users` rows from the Management API response — so pre-provisioning exists,
keyed on `(oidc_iss, oidc_sub)`, and the company roster is no longer re-entered
by hand. **The rule this section used to state — nothing creates an account
ahead of its owner's first login — was deliberately reversed there**: it was
right while pp-hub owned identity, and wrong once the IdP is authoritative,
because the key is exact and comes from the system that issued it.

What is still managed by hand is everything below the company line. There is
no SCIM, no admin create-user, and **no group sync**: a Logto organization
template is tenant-wide and cannot express a per-space, user-created group, so
department structure that already exists in the IdP is still re-entered as
pp-hub groups and kept in step manually. Only `email`, `email_verified`,
`name`, `organizations` and `organization_roles` are read from the token.

Identity is keyed on `(oidc_iss, oidc_sub)`, but since M20 that key is no
longer the only way in: a miss falls back to the **verified** email and
re-points the existing account (`AdoptUserIdentity`), because `oidc_iss` is a
URL an operator can change and changing it used to make every returning user
look brand new — the insert then collided with the unique index on
`(tenant_id, email_hash)` and sign-in answered 500 for everybody. A genuine
collision answers `email-taken` on the sign-in page rather than a 500. That is
a recovery path for a changed issuer, and it is separate from the
pre-provisioning above, which only ever inserts an identity Logto has already
issued.

Both directions of that key are now gated on `email_verified`, which M21
made symmetric. The adoption path above always required a verified claim; the
ordinary hit did not — `resolveUser` passed the claimed address straight into
`TouchUserLogin` on every sign-in, so `users.email` was rewritten from
whatever the IdP said. That address is not decoration: it is what a direct
share is granted to, what the member-add path resolves against an upstream
account, and what `PP_BOOTSTRAP_ADMIN_EMAIL` is compared against. At a
self-service IdP a user who could set their own address could therefore set it
to the bootstrap admin's and be promoted, or to a colleague's and be handed
what was meant for them. Now an unverified claim leaves the stored address
alone and logs that it was ignored; only a verified one updates it.
`email_verified` itself
is decoded through a `boolish` type, because some providers send it as the
string `"true"` and a strict decode would fail the whole claims read and take
every other claim down with it.

Deprovisioning is worse, and **M28 does not cover it**: `store/queries/users.sql`
has no `DeleteUser`. Suspension (`handleAdminUpdateUser`) is the terminal
state, and instance-level ownership transfer does not exist —
`spaces.RemoveMember`'s archive behaviour is per-space only. A directory sync
that can create accounts and cannot retire them is half a sync, so this is
either scope M28 grows or a gap that outlives it.

### Smaller, but on the list

Two of these are **M25** and the rest have no milestone at all — said plainly
here so they are not quietly absorbed into one that does not cover them.

- **No antivirus or content scanning.** Uploads go from the multipart part
  straight into the CAS (`fs.go:125-128` → `blob/blob.go:44-74`) with no scan
  hook, no MIME sniffing (type is guessed from the extension, `fs.go:139-142`)
  and no extension blocklist. Unscheduled.
- **No encryption at rest.** Local blobs are plaintext under `0755` dirs
  (`blob/blob.go:29-40`); the S3 backend sets no SSE headers; Postgres runs
  `sslmode=disable` inside the compose network (the `PP_DATABASE_URL` both
  the `server` and `worker` services are given in `compose.prod.yml`).
  Share tokens and session tokens _are_ stored hashed. Unscheduled.
- **Security headers shipped in M21, with two deliberate exceptions and one
  still to fix.** `apps/web/nginx.conf` now sends CSP, `X-Frame-Options:
DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, HSTS and
  `X-Content-Type-Options: nosniff`, all with `always` so they survive the
  502s the config is designed to return, plus `server_tokens off`. Two CSP
  directives are looser than the standard advice on purpose — `object-src
'self'` because the PDF viewer is an `<object>`, and `form-action` /
  `frame-src` naming the Document Server's origin because the
  editor handoff is a real cross-origin form POST. The third is not
  deliberate: `style-src 'self' 'unsafe-inline'`, repeated on all three
  `add_header` blocks in that file, is there for a handful of React `style`
  props and sonner's runtime `<style>` injection, so tightening it is an
  `apps/web` change rather than an nginx one — which is exactly why it is
  still here and unscheduled. See [deployment.md](../deployment.md).
- **No metrics, tracing or error tracking — M25.** Grep
  `prometheus|otel|opentelemetry|pprof|sentry` over `apps/server` and `deploy/`
  returns nothing. `log/slog` runs on the default text handler with no level
  control — no `slog.New` or `slog.SetDefault` anywhere — and chi's request id
  reaches `security_events` rows (`audit/audit.go:98`) but never a log record,
  so the one identifier that could join a log line to an audit row exists on
  only one side of the join.
- **`/healthz` was a static 200 — the split closed alongside M22–M24.**
  `/healthz` is still a static 200 — that is correct, it answers "is the
  process up", which is what a restart policy needs — but it was also what
  both the server `Dockerfile` and `compose.prod.yml` pointed their
  healthcheck at, so a container that could not reach Postgres reported
  healthy. `GET /readyz` (`httpapi/health.go`) now checks `pool.Ping` and
  the new `blob.Store.Check` with a 3s timeout and answers 503 on either
  failure; both the image and compose healthchecks point at it instead.
  `cmd/worker` still has no listener, so it cannot answer an HTTP probe at
  all — it now touches `/tmp/worker-healthy` every 30s while Postgres and
  the blob store both check out and removes it when they do not, and the
  worker's `HEALTHCHECK`/compose healthcheck reads the file's freshness with
  `find -mmin`. Verified live: `/readyz` returned 200 with Postgres up, 503
  within 3s of it being stopped, and 200 again once it came back.
- **Avatar upload was a stub behind a working-looking button — closed
  alongside M22–M24.** `handleUploadAvatar` bounded the body, parsed the
  multipart form, opened the file — then discarded it, stored the constant
  string `/api/v1/users/me/avatar` and answered 200. Nothing served that
  path; the control reported success while doing nothing, which is worse
  than a missing feature because nobody discovered it wasn't there. It now
  sniffs the content type from the bytes (never the filename — the same
  value is served back, so a caller who chose it could serve HTML from this
  origin), stores through the same `Put` → `UpsertBlob` → `IncrBlobRef`
  sequence `fs.PutFile` uses, decrements the previous avatar's ref only
  after the new one is live, and serves it back from `GET
/api/v1/users/{userID}/avatar` with `nosniff` and a cache-busting `?v=`
  suffix. `TestAvatarRoundTripsThroughTheBlobStore` proves the round trip
  byte-for-byte against a real Postgres and rejects a non-image upload with 415.
- **The UI is localized — closed by M32.** Lingui + gettext `.po`, English and
  Vietnamese, with catalogs at `apps/web/src/locales/` compiled into the
  bundle by `@lingui/vite-plugin`. Language is an account preference on the
  existing `users.settings` JSONB column, read off `GET /users/me` so it is
  known before first paint and written through
  `PATCH /api/v1/users/me/preferences`; the OnlyOffice ribbon follows via
  `?ui=` on the edit session. Translation runs in a self-hosted Weblate,
  provisioned by `deploy/weblate/` — a compose stack plus
  `pp-weblate-setup.sh`, which creates the project and component over the
  REST API so the configuration is reviewable rather than typed into a form.
  It is deliberately outside the product stack; a self-hoster running the
  drive never starts it. What is _not_ covered: the server still emits
  English only — problem+json details and `/login?error=` codes, and the
  organization invitation is whatever Logto's template says — though the
  client already turns those codes into translated
  sentences. See [i18n.md](i18n.md).
- **No orphan-blob audit.** `gc.Worker.Sweep` deletes the blob row before the
  file (`gc/gc.go:64-67`) — deliberately, since the other order leaves a row
  promising bytes that are gone, which reads as corruption — so a crash
  between the two leaves a file with no row, and the comment defers it to "the
  next full audit". There is no such audit: nothing walks the blob directory
  against the `blobs` table. Unscheduled; the leak is bounded by how often the
  process dies mid-sweep, which is also why it has never been urgent.

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

Nothing below is scheduled; it is the order the dependencies suggest. What has
changed is that each phase now has named milestones carrying it, and those
live in [milestones.md](milestones.md) under `## Upcoming — M25 Onward`. The
division is deliberate: that document is the plan, this one is the evidence,
and a milestone that cannot point back to a section here has not been argued
for.

**Phase A — rollout blockers — done.** P0 items 1–8, all closed: M20 brought
the `sessions` table, M21 added `security_events`, so the schema changes this
phase ever needed were already applied; the Document Server runs signed, the
first admin is a configuration value, upload bodies and request rates have
ceilings, and the sweep findings in §8 are closed. What remained was the work
that was not code in this repository — **M22** (§1, backup and restore, now a
drilled restore) and **M23** (§2, mail transport, invitations only — since
deleted by M33, which moved that email to Logto) — plus
**M24** (§7), whose decision landed as refuse-not-lock: the REST
version-upload and version-restore paths now answer 409 against a live lock
rather than taking one of their own, which is simple, correct, and
occasionally makes an editor left open over lunch block a colleague with no
recourse short of the TTL. Along the way, two items that were never on the P0
register but read like rollout blockers to anyone testing the product closed
too: the readiness split under "Smaller, but on the list" (`/readyz`, and a
file-based signal for `cmd/worker`) and the avatar-upload stub.

**Continuous — M25, observability.** What remains of it after the readiness
split above is metrics, tracing, error tracking, and a level-controlled
`slog` handler carrying the request id. It is cheap at any point and it makes
every phase after it diagnosable, which is the argument for doing it early;
it sits between the phases rather than inside one only because nothing
depends on it. Its evidence is the "No metrics, tracing or error tracking"
bullet under "Smaller, but on the list".

**Phase B — the multi-department layer.** **M26 — audit completeness** is
partly built: `security_events` carries authentication, admin, suspension,
grant, share and membership changes with request context and a retention
window, so what is left of it is read auditing (a design decision, not an
oversight — see the section above), an export endpoint, and anything that
watches the log rather than waiting to be read. Then **M27 — content-indexed
search** as a new preview-pipeline job kind, then **M28 — directory sync**,
which is the pre-provisioning and IdP-group-sync half of "Identity is managed
by hand". These are what changes when the user count crosses a couple of
departments, and each one is additive to a pattern that already exists.

**Phase C — collaboration and storage honesty.** **M29 — comments and
notifications**, which is now the whole of that problem rather than half of
it: M33 deleted the transport M23 built, because the invitation it carried
moved to Logto, so M29 owns the notification story, what a share or comment
notification says, the `notifications` table _and_ the transport that delivers
any of it; M30 — resumable upload and honest accounting, which
is the dead `uploads` table, version retention and a quota that counts every
referenced blob; and **M31 — document creation** from a template.

**Named but unscheduled.** A gap with no milestone is still a gap, and the
risk of a roadmap is that it stops being visible: `style-src
'unsafe-inline'`, the orphan-blob audit `gc.go` defers to, upload scanning,
encryption at rest and an English-only UI. All five keep their evidence in
"Smaller, but on the list", and [milestones.md](milestones.md) lists them
under a heading of the same name, so a reader working from the plan meets
them too.

## Verdict

A pilot with one department is reasonable now that Phase A is done: a backup
that has been restored from, a locked file that a REST upload cannot silently
overwrite, a
`/readyz` that actually fails when a dependency has, and an avatar upload
that survives a reload — provided everyone understands that search finds
names rather than contents and that there are no comments. A company-wide
rollout across several departments needs Phase B as well: M27's content
search, and the half of the audit story M26 still owes, the half that
answers "who read this", which is the question that gets asked within the
first week rather than the first month.
