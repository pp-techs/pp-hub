# Production Readiness — Gap Analysis

M1–M19 delivered the product; M20 closed one of the items below, M21 closed
three more plus a set of things nobody had written down, and M22–M24 closed
the rest of the P0 register — backup and restore, an invitation mail
transport, and the REST/WOPI write collision — along with two items that
were never on it: the readiness split and the avatar-upload stub. **M33 then
un-closed half of it** (membership moved to a Logto organization, and the space
invitation went with it), and **M31 reinstated that transport for share
notifications**, configured in Admin › Settings by **M35**. This document is
what stood between that product and an internal rollout, assessed against the
target deployment: **one company, 20–100 users, several departments,
self-hosted, no compliance auditor yet**.

It is the evidence, not the plan. [milestones.md](milestones.md) holds the
plan: every remaining gap below is carried by a named milestone, **M28**
(M27, M29, M30 and M31 now closed), and each heading here says which.
Nothing there is scheduled — the numbers are an order the dependencies
suggest, and the phasing at the end explains that order. What stays here is
the reason each item is (or was) on the list and the `file:line` that lets a
reader check whether it is still true before acting on it; where a line number
would only rot, the function or query is named instead. Closed items shrink to
a row pointing at the milestone that owns the evidence, so this page answers
"what is still open" first.

The headline: the drive itself is finished, and as of M22–M24, so is the
operational layer that makes it safe to put real work in it — a restorable
backup, a readiness probe that actually fails when a dependency has, and a
REST write that cannot silently discard an open editor's work. The permission
model (M15) is stricter by default than Google Drive's, WOPI co-editing is
real, and versioning, trash retention, blob GC, quota and search all work.
What was missing is the layer above that: content search, richer audit,
directory sync, comments, and honest storage accounting. Audit (M26),
comments (M29), accounting (M30) and content search (M27) are closed;
directory sync remains.

## P0 — blocks an internal rollout

These were not features. Each was either a way to lose data, a way to leak it, or
a step an administrator could not perform at all — and **all eight are closed**.
The register stays because a list of closed findings is how the next reader learns
which mistakes this codebase is prone to; the evidence now lives with the
milestone that closed it.

| #   | What it was                                              | Closed in                              | Evidence                                              |
| --- | -------------------------------------------------------- | -------------------------------------- | ----------------------------------------------------- |
| 1   | No backup of any kind                                    | M22                                    | [M22](milestones/M22.md)                              |
| 2   | No mail transport (invitations only)                     | M23, deleted by M33, reinstated by M31 | [M23](milestones/M23.md)                              |
| 3   | A session was a signature, with nothing to revoke        | M20 and M21                            | [M20](milestones/M20.md)                              |
| 4   | The Document Server was reachable and unsigned           | M20–M21                                | below, [office-integration.md](office-integration.md) |
| 5   | The first admin could not be created                     | M21                                    | [M21](milestones/M21.md)                              |
| 6   | Uploads were unbounded and nothing was rate limited      | M21                                    | [M21](milestones/M21.md)                              |
| 7   | A REST upload silently overwrote an open editing session | M24, re-cut by M30                     | [M24](milestones/M24.md)                              |
| 8   | What two read-only sweeps found                          | M21, M26                               | [M21](milestones/M21.md), below                       |

### 4. The Document Server — what the fix requires of a deployment

The deployment still carries what makes this safe, and an operator editing it
should know why.

- **Requests to the Document Server are signed**: `JWT_ENABLED=true` with a shared
  `PP_ONLYOFFICE_JWT_SECRET`, and `office.SignJWT` signs every
  request to it — the worker's conversions and the server's forcesave commands —
  body `token` claim **and** `Authorization: Bearer`, because
  the docs disagree about which endpoints honour which. Unsigned requests come
  back `-8` with no fetch attempted, verified against a live 10.0.0.1.
- **`ALLOW_PRIVATE_IP_ADDRESS=true` stays, and it is load-bearing**: the WOPI host
  sits at a container address. It is tolerable only because of the line above.
  **`ALLOW_META_IP_ADDRESS` is gone from production** — that range contains
  `169.254.169.254` and nothing in this topology lives there — and only
  development still sets it, only for the live conversion test that fetches
  from `host.docker.internal`.
- **OnlyOffice 10 needs a rule naming the WOPI host.** The `onlyoffice`
  entrypoint writes an ipfilter rule file allowing `server` and refusing `*`,
  which also means the Document Server can only be asked to fetch from that
  host.
- **A WOPI token is a ceiling, not an authority.** `wopiClaims` re-resolves on
  every request (node, `users.suspended_at`, `EffectiveCapsForNode`) and clamps
  the token's rank, refusing at rank 0. A revoked edit grant downgrades an open
  session to read-only — `CheckFileInfo` reports the live `UserCanWrite`, so the
  editor switches itself — rather than killing it, and `PutFile` refuses a node in
  the trash so a save cannot resurrect a tombstone. A machine token has no subject
  to resolve and is authorized by the signing key alone. Pinned by
  `internal/httpapi/wopi_db_test.go`.

### 8. What the sweeps found, and the shape of the bugs

Two read-only sweeps went through the server and the deployment looking for what
the register had not thought to list. All thirteen findings are fixed, and they
were kept for one reason: several are the same mistake wearing different clothes.
The classes matter more than the individual fixes, because the fix is local and
the class is not.

- **A read path that did not re-check what it depended on.** A link checked its
  token and expiry, not the grant behind it; a WOPI token carried the rank from
  when the editor opened. Both now re-resolve on read, which covers every way
  access can disappear — including an ancestor being moved — instead of one
  cleanup path per way.
- **A label trusted as a permission.** "View only" served byte-identical
  originals and a verbatim 4 KiB text "preview"; rank 1 could `GetFile`. A label
  is not a predicate.
- **Two paths validating one value differently.** Sign-in rewrote `users.email`
  from an unverified claim while the adoption path required `email_verified` — and
  that column decides who a share is granted to, and what
  `PP_BOOTSTRAP_ADMIN_EMAIL` matches.
- **A header believed from any source.** chi's `RealIP` accepted
  `True-Client-IP`, `X-Real-IP` and `X-Forwarded-For` from anybody, which made
  every address-keyed limit resettable at will; it is replaced by
  `middleware.ClientIPFromXFF(PP_TRUSTED_PROXIES…)`, read everywhere through
  `audit.ClientIP`.
- **A control that was only a browser policy.** `SameSite=Lax` was the entire
  CSRF defence until `sameOrigin` existed.
- **Authorising on one row and mutating another.** `DELETE
/nodes/{id}/shares/{shareID}` checked `CapShare` on the node in the URL, then
  deleted by share id alone.
- **A privilege check that implied membership.** `spaces.SetAdmin` seated a
  non-member, producing an admin the People page does not list.
- **An allowlist containing a catch-all.** `isOfficeMime` accepted
  `application/octet-stream`, which is what an unrecognised extension is stored
  as, so the allowlist read "any file with an unfamiliar name".
- **The same secret in two places it should not be.** Share tokens and WOPI
  `access_token`s were written to the access log, when only their hashes are
  stored; and `apps/server/.env` plus `data/blobs` were pulled into builder layers
  because Docker reads `.dockerignore` from the build context root, not the
  repository root.

The individual fixes are recorded in [M21](milestones/M21.md) (link, WOPI rank,
preview, IP provenance, CSRF, log redaction, `.dockerignore`, network
segmentation, `nosniff`) and [M20](milestones/M20.md) (the email asymmetry). Two
items closed alongside M22–M24 have no milestone of their own and stay under
"Smaller, but on the list" below: the `/readyz` split and the avatar stub.

### Still open from this register

- **No write or idle timeout on the HTTP server** — only
  `ReadHeaderTimeout: 10s` (`cmd/server/main.go`). Long uploads and OnlyOffice
  conversions need long requests, and the stack has no proxy to impose limits
  now, so a blanket handler timeout is not a one-line
  change. No milestone carries it; it belongs to whoever next touches the
  server's lifecycle.
- **No per-user quota and no per-space ceiling** beyond `quota_bytes`. The
  per-request ceiling bounds one request, not a thousand, and a space with
  `quota_bytes = 0` is unmetered by definition; `users.quota_*` does not exist.
  The accounting half is under P1 below.

## P1 — the gap against Google Drive

Ordered by what a 20–100 person, multi-department deployment feels first. Each
heading names the milestone that carries it; the phasing at the end places
that milestone in a phase.

### Search only matches file names — closed in M27

**Markdown, plain-text, PDF and office files are content-searched since M27**
(migration `0016_content_search.sql`): an extraction job writes each file's
current text into `node_texts`, folded so Vietnamese matches without accents,
and `GET /api/v1/search` merges content hits with name hits — every name hit
first — returning `match`, `snippet` and `snippet_highlights`. Existing files
are backfilled by `cmd/server` at startup. Access filtering is unchanged. See
[preview-pipeline.md](preview-pipeline.md#text-extraction) and
[M27](milestones/M27.md).

**What is still matched by name only:** images and scanned PDFs (no OCR), and
formats with no extractor (`.rtf`, `.csv`, `.epub`, archives). Office and PDF
text arrives only while a `cmd/worker` runs — it needs poppler and OnlyOffice —
so an install without one matches those files by name, as it did before. The
original gap was `pg_trgm` and one GIN index on `nodes.name`
(`nodes_name_trgm`, partial on `deleted_at IS NULL`), a prefix match ranked
over a mid-string one, trigram similarity as the fallback.

Google Drive indexes document contents and OCRs images. For a company document
store this is the difference between "find the contract" and "remember which
folder the contract is in" — and the second one stops working at about the
point a second department joins.

The office half was an addition to a working path, and stayed one: the
extraction registry routes by job kind, office documents already passed through
OnlyOffice conversion in `cmd/worker`, and the extractor registered there
needed no change to storage, backfill or search.

### `activity_events` is a feed, not an audit log — closed in M21 and M26

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
| Reads and downloads          | **Closed in M26** — `file_downloaded`, `folder_downloaded`, `file_viewed`, `file_opened`, deduplicated to one row per (kind, actor, node) per hour, with their own retention                    |
| Authentication events        | **Closed** — `login`, `login_failed`, `logout`, `session_revoked`, `sessions_revoked`, `identity_adopted`, written by `internal/auth`                                                           |
| Permission and admin changes | **Closed** — `admin_granted`, `admin_revoked`, `account_suspended`, `account_unsuspended`, `grant_created`, `grant_revoked`, `share_created`, `share_revoked`, `member_added`, `member_removed` |
| Request context              | **Closed** — every row carries `ip` (through `audit.ClientIP`), a 400-byte-truncated `user_agent` and chi's `request_id`, so a row joins to the access-log line that produced it                |

`GET /api/v1/admin/security-events` pages the log newest first, admin-only and
deliberately **not** space-scoped: the whole value of a run of failed sign-ins
is seeing it across accounts and addresses at once. `?kind=`, `?node=` (the
node and its subtree), `?from=`/`?to=` filter,
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

**Read auditing was deferred in M21 on purpose, and M26 made the decision it
was waiting on.** A row per byte-serving request would be the highest-volume
event in the system — every thumbnail in a grid scroll, every range request a
video player makes, every WOPI `GetFile` behind an editor keystroke. M26
records only _intentional_ reads (a download, a folder zip, an inline view, an
editor open) and not thumbnails or `GetFile`, and absorbs the rest in memory:
one row per (kind, actor, node) per hour, per replica. Read kinds are purged
on their own, shorter retention (`read_audit_retention_days`, 90). So "who
downloaded the payroll folder?" now has an answer: click the folder's name in
the console, or export it — `GET /api/v1/admin/security-events/export` streams
the filtered log as CSV. The residual imprecision is stated rather than
hidden: a link visitor is identified by link and address, so two visitors
behind one NAT are one row per hour.

Nothing watched the log on its own; `internal/alert` now does, for one rule —
a burst of failed sign-ins from one address raises a `security_alert` row, a
`pphub_security_alerts_total` increment and an email to the instance admins.

### No comments — closed in M29

Drive's primary collaboration surface, deferred at every milestone since M10.
M29 built it for **block documents**: threads anchored in the markdown by a
comment marker, `comment_threads`/`comments`/`notifications` (`0015`), REST
routes under `CapView`, a comments panel in the editor, and in-app
notifications — a bell with an unread badge, delivered live over Server-Sent
Events — for comments, replies and @mentions in comments or in the document
text. Deliberately not built: comments on office files and any email for comments or mentions. See
[milestones/M29.md](milestones/M29.md).

### Uploads are not resumable — closed in M30

What it was: the `uploads` table is in the schema (`0001_init.sql:229-242`)
and **no Go code referenced it** — it was dead schema. The transfer tray's
pause stopped the queue rather than a transfer in flight, which M11's entry in
[milestones.md](milestones.md) records as still-not-done. A 4 GB upload that
dropped at 90% restarted at zero, and nothing swept abandoned rows because
nothing wrote them. In production it was worse than a missing feature: one
multipart POST behind Traefik's 60-second `readTimeout` or Cloudflare's
100 MB body cap failed outright for any file that took longer than a minute
(see the Dokploy section of [deployment.md](../deployment.md)).

What shipped: tus 1.0.0 (core, creation, termination) at `/api/v1/uploads`,
replacing both multipart endpoints (`internal/httpapi/uploads.go`,
`internal/uploads`, migration `0010_resumable_uploads.sql`). The cap and the
quota — including bytes other in-flight uploads have promised — are checked
against the declared length before a byte is accepted; an interrupted PATCH
keeps every byte that arrived, fsynced before the offset advances; the final
PATCH commits before answering, and a committed row outlives the commit so a
client that lost the response learns from HEAD that the file landed.
Staging lives in `<PP_BLOB_ROOT>/.uploads`, an upload expires after 24 hours
without a byte, and a 15-minute sweep removes expired rows, their files and
orphaned files. The web client (`apps/web/src/lib/api/tus.ts`) sizes chunks
from measured throughput toward 15 s per PATCH and retries proxy failures
with a HEAD resync; the tray's pause now stops a transfer in flight and
resume continues from the server's offset.

What honestly remains: a per-upload lock that is in-process and staging on
local disk, so the upload routes assume one `server` process; and the
browser keeps an upload's URL in memory only, so a page reload starts the
file over (the abandoned upload expires on its own).

### Storage accounting understates real usage — closed in M30, except per-user quota

What it was: `node_versions` grows without bound (`0001_init.sql:207-220`);
nothing pruned old versions and there was no version-count cap. Quota,
meanwhile, was computed from the current version only
(`fs.Service.checkQuota`, `fs.go:175-202`: it summed space usage and then
subtracted the outgoing current version's size, so every superseded version
was invisible to the meter that is supposed to bound the disk).

The two together meant the disk filled while the storage meter said there was
room — the numbers were not merely incomplete, they diverged over time. Either
quota counts every referenced blob, or versions get a retention policy;
ideally both.

What shipped: both. `SumSpaceUsage` counts every stored version of every file
in the space, trash included, and migration `0010` recomputed every space's
`used_bytes` under that definition; `fs.EnsureRoom` charges a new version its
full size. `version_retention_count` (Admin › Settings, default 100, `0`
keeps every version) prunes a file's history beyond the newest N on each new
version, releasing the blobs to GC — on the next save of each file, not in a
retroactive sweep. `GET /api/v1/usage` breaks out `versions` and `trash`, so
the slices sum to the meter.

Still absent: per-user quota (no `users.quota_*` column,
`0001_init.sql:33-50`), file-count limits, and any egress accounting. Usage
also sums version sizes, not distinct blobs, so deduplicated content (a copy
shares its blob) is counted once per version that references it.

### No way to create a document — closed in M31

Grep for a blank-document template or a create-office-file path across `apps`
returned nothing: every `.docx` had to be created somewhere else and uploaded
before it could be co-edited, which made the editor a viewer with write access
rather than a place work starts. Closed by `POST
/api/v1/spaces/{id}/documents`, which writes a blank document, spreadsheet or
presentation from `internal/office/templates.go` into the write target M19
resolves and opens the editor on it.

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

The two that were **M25** are closed and the rest have no milestone at all —
said plainly here so they are not quietly absorbed into one that does not
cover them.

- **No antivirus or content scanning.** Uploads went from the multipart part
  straight into the CAS (`fs.go:125-128` → `blob/blob.go:44-74`) with no scan
  hook, no MIME sniffing (type is guessed from the extension, `fs.go:139-142`)
  and no extension blocklist. Since M30 they arrive as a tus staging file and
  go into the CAS through `fs.PutStagedFile` → `blob.Store.Ingest` (WOPI saves
  still stream through `fs.PutFile` → `Put`); neither path scans or sniffs.
  Unscheduled.
- **No encryption at rest.** Local blobs are plaintext under `0755` dirs
  (`blob/blob.go:29-40`); the S3 backend sets no SSE headers; Postgres runs
  `sslmode=disable` inside the compose network (the `PP_DATABASE_URL` both
  the `server` and `worker` services are given in `compose.prod.yml`).
  Share tokens and session tokens _are_ stored hashed. Unscheduled.
- **Security headers shipped in M21, with two deliberate exceptions and one
  still to fix.** The server (`internal/httpapi/web.go`; M21 originally set
  them in nginx) now sends CSP, `X-Frame-Options: DENY`,
  `Referrer-Policy: strict-origin-when-cross-origin`, HSTS (only when
  `PP_EXTERNAL_URL` is https) and `X-Content-Type-Options: nosniff` on every
  response. Two CSP
  directives are looser than the standard advice on purpose — `object-src
'self'` because the PDF viewer is an `<object>`, and `form-action` /
  `frame-src` naming the Document Server's origin because the
  editor handoff is a real cross-origin form POST. The third is not
  deliberate: `style-src 'self' 'unsafe-inline'`
  is there for a handful of React `style`
  props and sonner's runtime `<style>` injection, so tightening it is an
  `apps/web` change rather than a deployment one — which is exactly why it is
  still here and unscheduled. See [deployment.md](../deployment.md).
- **No metrics or structured logging — closed in M25; tracing and error
  tracking deliberately left out.** `log/slog` ran on the default text
  handler with no level control, and chi's request id reached
  `security_events` rows but never a log record. `internal/obs` now installs
  a level/format-controlled handler in both binaries that adds the request id
  to every record logged with a request context, the id is returned as
  `X-Request-Id`, and the server exposes Prometheus metrics on a separate
  internal listener (`PP_METRICS_ADDR`, default `:9091`). Tracing and error
  tracking were scoped out of M25 and remain unscheduled.
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
  origin), stores through the same claimed `Put` `fs.PutFile` uses — the
  `users.avatar_blob` column is the reference, counted by trigger since
  M37 — and serves it back from `GET
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
- **No orphan-blob audit — closed by M37**, along with what a review of the
  local store found beside it: GC could collect a blob an upload had just
  deduplicated against (reproduced; the new file pointed at nothing),
  `ref_count` could end up too low because it was moved by hand outside the
  referencing statements, a write refused after its bytes landed leaked them
  with no row for GC to find, previews were never counted, nothing fsynced a
  blob before its reference committed, a damaged copy was deduplicated against
  forever, and nothing ever re-read a stored blob. Now the store claims a
  pinned row before installing bytes, triggers own `ref_count`, GC deletes row
  and bytes in one transaction, blobs and their directories are fsynced, and
  `gc.Worker` runs a daily audit (tidy, recount, adopt) and an hourly scrub.
  See [storage.md](storage.md#garbage-collection) and
  [M37](milestones/M37.md).

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
live in [milestones.md](milestones.md) under `## Upcoming — M28 Onward`. The
division is deliberate: that document is the plan, this one is the evidence,
and a milestone that cannot point back to a section here has not been argued
for.

**Phase A — rollout blockers — done.** P0 items 1–8, all closed: M20 brought
the `sessions` table, M21 added `security_events`, so the schema changes this
phase ever needed were already applied; the Document Server runs signed, the
first admin is a configuration value, upload bodies and request rates have
ceilings, and the sweep findings are closed. What remained was the work that was
not code in this repository — **M22** (backup and restore, now a drilled
restore) and **M23** (mail transport, invitations only — since deleted by M33,
which moved that email to Logto, and reinstated by M31 for share notifications)
— plus **M24**, whose decision landed as refuse-not-lock: the REST version-upload
and version-restore paths now answer 409 against a live lock (since M30 the
version upload is a tus replacement, answering 423) rather than taking one of
their own, which is simple, correct, and occasionally makes an editor left open
over lunch block a colleague with no recourse short of the TTL. Along the way,
two items that were never on the P0 register but read like rollout blockers to
anyone testing the product closed too: the readiness split under "Smaller, but on
the list" (`/readyz`, and a file-based signal for `cmd/worker`) and the
avatar-upload stub.

**Continuous — M25, observability — done.** A level-controlled `slog` handler
carrying the request id, a structured access log and a Prometheus `/metrics`
listener on an internal port; see the "No metrics or structured logging"
bullet under "Smaller, but on the list". Tracing and error tracking were left
out of it by decision and have no milestone.

**Phase B — the multi-department layer.** **M26 — audit completeness — done**:
`security_events` now records intentional reads as well as access changes,
filters by node and date, exports as CSV, and alerts on bursts of failed
sign-ins; see the audit-log section above. Then **M27 — content-indexed
search** as new preview-pipeline job kinds — **done** for markdown, plain
text, PDFs and office documents — then **M28 — directory sync**, which is the
pre-provisioning and IdP-group-sync half of "Identity is managed by hand".
These are what changes when the user count crosses a couple of departments,
and each one is additive to a pattern that already exists.

**Phase C — collaboration and storage honesty — done.** **M29 — comments and
notifications — done**: comments on block documents with in-app notifications
over a live stream, beside the share-notification and alert mail M31 and M35
put back after M33 deleted M23's transport; **M30 — resumable upload and
honest accounting — done**: tus uploads against the formerly dead `uploads`
table, version retention, and a quota that counts every stored version; and
**M31 — document creation — done** from a template.

**Named but unscheduled.** A gap with no milestone is still a gap, and the
risk of a roadmap is that it stops being visible: `style-src
'unsafe-inline'`, upload scanning and encryption at rest. (Two more, the
English-only UI and the orphan-blob audit, were closed by M32 and M37.) All of
them keep their evidence in "Smaller, but on the list", and
[milestones.md](milestones.md) lists them under a heading of the same name, so a
reader working from the plan meets them too.

## Verdict

A pilot with one department is reasonable now that Phase A is done: a backup
that has been restored from, a locked file that a REST upload cannot silently
overwrite, a
`/readyz` that actually fails when a dependency has, and an avatar upload
that survives a reload — provided everyone understands that search finds
images and scans by name only, that PDF and office text is indexed only while
a worker runs, and that comments exist only on block documents. A company-wide
rollout across several departments needs the rest of Phase B as well, M28's
directory sync above all; the audit half — "who read this", the question that
gets asked within the first week rather than the first month — M26 answered.
