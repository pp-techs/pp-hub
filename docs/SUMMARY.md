# Documentation Summary

pp-hub — a minimal, self-hosted Google Workspace alternative: Drive, collaborative office (OnlyOffice/WOPI), permissions, and multiple spaces.
Monorepo: bun workspaces + Vite+ frontend (`apps/web`), Go modular-monolith backend (`apps/server`), shared UI package (`packages/ui`), Postgres + content-addressed disk storage, compose variants in `deploy/compose/`.

## Agent Context Guide

Before planning or implementing, read this `docs/SUMMARY.md` file first. Load only the detail docs relevant to the current task, and prioritize `Code Standard` docs for implementation conventions. If docs conflict with code or user intent, use the available question tool before making broad changes.

**There is a roadmap — read it before inventing one.** `## Upcoming — M22 Onward` in [milestones.md](architecture/milestones.md) names M22–M31 and the phase each belongs to, and [production-readiness.md](architecture/production-readiness.md) holds the evidence behind them, down to the `file:line`. A request that sounds like a new feature is usually a milestone that already exists with a decided shape and a stated reason for its position; the gaps deliberately left without a milestone are named there too, so "nobody has written this down" is worth checking before it is assumed.

Status: M1–M21 done, M22–M31 open — full MVP: OIDC auth (Logto; any OIDC provider works — PKCE and nonce on the handshake, sessions as rows so a device can be signed out on its own), drive (spaces/nodes/CAS blob/upload/download/versioning), sharing (direct + link), OnlyOffice WOPI editing with locks, previews via background jobs — images, PDFs, video and office documents all get a real 256px thumbnail, text gets a 4KB snippet. PDF/video rasterization needs poppler/ffmpeg, and office documents convert through OnlyOffice first; all three live in an **optional** `cmd/worker` container; without it those jobs stay pending rather than failing, and `worker --backfill` sweeps files that predate it. Full-size viewing still streams the original bytes from `GET /api/v1/nodes/{id}/content` (inline for an allowlisted MIME, attachment otherwise), since a 256px thumbnail is a stamp in a modal. Two compose stacks in `deploy/compose/` — `compose.dev.yml` (services only, app from source) and `compose.prod.yml` (everything, VPS/Dokploy); the web image is nginx serving the SPA and proxying the API on one origin, which the session cookie requires. See [deployment.md](deployment.md). UI primitives extracted to `packages/ui` (shadcn/ui preset). Drive rail exposes Trash and Shared with me as routes under the open space (`/drive/$spaceId/trash|shared`); Trash supports restore, delete forever and empty trash. Uploads run through a queue with drag-and-drop, folder upload and a progress tray. ⌘K opens a command palette backed by `GET /api/v1/search` (trigram index, scoped to your space membership). Share links browse shared folders anonymously (containment-checked, zip download); stars, activity and a Home dashboard are scoped to the open space; space settings cover slug/description/quota, whether new members arrive as admins, and deletion, with members, their groups and pending invitations in one filterable table (admin, joined, last active, files owned, CSV export); the admin console reports server stats, can suspend accounts, and reads the security log (`GET /api/v1/admin/security-events`); the account page lists your live sessions and can end them. Blobs live on local disk or in any S3-compatible bucket (`PP_BLOB_BACKEND`), both behind one interface and one conformance suite (SeaweedFS ships in the compose files). Blob GC reclaims disk from unreferenced content, quotas are enforced at upload, and per-space trash retention purges on a schedule (off by default). Every mutation is same-origin checked, the sign-in, share-link and API surfaces are rate limited per IP, upload bodies are capped by `PP_MAX_UPLOAD_BYTES`, and the first admin comes from `PP_BOOTSTRAP_ADMIN_EMAIL`.

**Not done, and each one is felt before a pilot rather than after it.** There is **no backup procedure** (**M22**): nothing in `deploy/` dumps Postgres or snapshots the blob volume, and the two have to be captured together — a database restored without its blobs is a directory listing of files that no longer exist, which makes this the one gap whose absence is irreversible. There is **no mail transport** (**M23**): `handleCreateInvitation` returns the invite URL in its JSON response for an admin to copy and paste, sharing notifies nobody, and there is no in-app substitute, so nothing here reaches a person who does not already have an account. And a **REST version upload ignores `file_locks`** (**M24**): the table exists and the WOPI endpoints honour it, but `GetLock` has no caller outside `internal/wopi`, so `PUT /nodes/{id}/versions` overwrites whatever a colleague has open in the editor — the only remaining way this product loses data during ordinary, well-intentioned use. Everything after those three — observability, audit completeness, content-indexed search, directory sync, comments and notifications, resumable upload and honest storage accounting, document creation — is **M25–M31** under `## Upcoming — M22 Onward` in [milestones.md](architecture/milestones.md), with the evidence for every one of them, and for the gaps carrying no milestone at all, in [production-readiness.md](architecture/production-readiness.md).

**M11 — a space is a company.** One instance hosts several companies: space membership grants **no file access at all**, and access comes from `groups` granted on folders (`shares.kind = 'group'`), inherited down the subtree. Resolution is one batched recursive query that also walks through trashed ancestors, so the trash needs no weaker rule of its own. Listings, search, starred, recent, activity and trash all narrow to what the caller can reach, in one batched query per page rather than a walk per row, and `Shared with me` is an actual list of grants rather than a slice of the filing cabinet. Groups are managed from the drawer on the People page in space settings, and a folder's `Who can open this…` dialog is the one place access is granted or revoked; behind them sit `/spaces/{id}/groups` and `/nodes/{id}/grants`. A direct share may not name someone outside the space — that is what link shares are for.

**M15 — permissions are owner-anchored.** Access comes from exactly two things, both gated on **current membership of the node's space**: **ownership** (`nodes.created_by`, inherited down the subtree and resolved in the same recursive query as the grants, so it is not a revocable row and nobody can be locked out of their own work) and **grants**. Everything a person creates is private to them until they share it — and leaving the space ends their reach, so a departure files their work in the Archive instead of moving a folder they can still read. The role scale is gone — no `space_members.role`, no `default_role`, no `roleRank`, no `RequireRole` — and **admin no longer means access**: admin is membership in the space's automatic `Admin` group, it gates space administration via `spaces.RequireAdmin`, and it gates no file at all, because the old model handed every admin an edit floor over the whole company tree. `shares.perm` became `level` (`view` | `edit`) plus independent `can_download` and `can_share` flags — the wire carries that triple directly, since no single ranked word can say "view and pass on, but do not download" — with DB CHECKs keeping an editor able to download and links view-only and unchainable, and `Caps.Covers` refusing to hand out more than the sharer holds (403). `can_download` gates taking a copy, not seeing bytes: `/content` streams inline under view, `/download` needs the flag, for members and anonymous links alike. **Superseded by M21**, which found that handing over the complete file is a download whatever the `Content-Disposition` says — `/content` needs the flag too now, on both surfaces. Departure has an answer instead of a disclaimer: every space owns an `Archive` folder whose only grant is edit+download+share to the `Admin` group, and `RemoveMember` revokes the leaver's grants both ways, climbs nested ownership to the nearest ancestor owner, moves their remaining roots into the Archive and drops their group rows, refusing to lose the last admin (409). Personal folders retired — `personal_folders`, `spaces.personal_root_id` and the pinned "My files" are gone, a member's top level is `ListEntryPoints` (what they own or were granted), and a member can finally create a folder at their own top level. Node DTOs carry `caps`, so the UI offers exactly what the caller holds: sharing and the `Who can open this…` dialog follow `caps.share` rather than admin status, and any member may read the space's group list in order to grant one access. The fifteen development migrations were squashed into one baseline, `0001_init.sql`. See [permissions.md](architecture/permissions.md).

**M16 — login → account → space has one shape.** Every drive surface is a child route of `/drive/$spaceId/`: the listing plus `home`, `recent`, `starred`, `shared`, `trash` and `settings/*`. Before this, Home, Recent and Starred were siblings of `$spaceId`, so opening one emptied the rail — the switcher fell back to "Select a space", the folder tree disappeared and the Trash and Shared rows, which were `?panel=` values needing a space, vanished with it. `?panel=` is gone entirely, along with `type DrivePanel`. Space membership now has exactly one home: `settings/people` is one table carrying the Admin switch, group chips and pending invitations as rows, beside a group drawer keyed by `?group=` — replacing a `?panel=members` panel and a Members tab that rendered the same component at two addresses while the Groups tab insisted admin membership was "managed in the Members tab". Because admin _is_ membership of the automatic `Admin` group, membership mutations invalidate `["spaces", id]` rather than just the member list. Settings tabs are links, and a non-admin is refused in `beforeLoad` instead of being shown a "you cannot" pane at a URL that resolved; the standalone `/profile` page became `/drive/account/{profile,preferences}` inside the shell. `recent`, `starred` and `shared` gained the optional `?space=` that `activity` and `usage` already had, filtered in SQL because all three are limited and a post-filter would return a short page. Deep links survive sign-in: `?redirect=` on `/login` and a short-lived, revalidated `pp_oidc_next` cookie across the OIDC round trip, accepting same-origin paths only. Cross-space Home was removed rather than deferred — a row from another space cannot be opened without silently switching the space the rail is showing.

**M17 — one dialog owns access.** `Share` on any node — file or folder — is the single place groups, people and the public link are granted and revoked; the folder-only `Who can open this…` dialog and the email-only share panel are gone, along with `?panel=share` (`PanelKind` is now just `versions`). Grantees are picked from the space, never typed: one combobox offers the space's groups above its members, filtered locally because `GET /spaces/{id}/groups` and `GET /spaces/{id}/members` are both member-readable and already cached, with one level/download/re-share control applied to everyone picked. Group grants now reach files too — the server always allowed it (`Grant` only checks that node and group share a space, and a share row on a file is an entry point like any other), the client merely hid it; the same widening fixed a share list that rendered a group grant as a row labelled with the literal word "group". `GET /users/search` stays the invite path's tenant directory but is now scoped to accounts sharing a space with the caller, lifted only for an instance admin: it was an unscoped address book across every company on the instance.

**M18 — the shell is the shadcn dashboard shell.** `drive.tsx` is now
`SidebarProvider` → `app-sidebar` (`variant="inset"`, `collapsible="offcanvas"`)
→ `SidebarInset`, so the rail collapses with `Ctrl+B`, remembers that in a
cookie and becomes a sheet below `md` — none of which the hand-rolled
`<aside className="w-64">` could do. The bigger win is that there is now
**one** header: `site-header.tsx` renders the trigger, a breadcrumb derived
from the pathname, the ⌘K button and a portal slot, and every surface fills
that slot through `HeaderActions` instead of stacking a second 40px band of
its own — Home and Account each used to. `drive-header.tsx`, `drive-toolbar.tsx`
(dead: defined, never imported), `quick-access.tsx` and `rail-item.tsx` are
gone; rail rows are `SidebarMenuButton`/`SidebarMenuSubButton`, and `RailLabel`
— which was never used in the rail — is `section-label.tsx`. Page rhythm has
one home, `surface.tsx` (`@container/main`, `px-4 lg:px-6`, `py-4 md:py-6`),
and the folder listing deliberately opts out of it: a file manager's rows reach
the edges and scroll under a sticky header, so it keeps a full-bleed container
clipped to the inset's corner. Two invariants are load-bearing —
`SidebarInset` has to stay a direct sibling of the sidebar (its margin and
rounding are `peer-data-[variant=inset]`), and the shell has to pin `h-svh
overflow-hidden`, because `SidebarProvider` is only `min-h-svh` and the marquee
band measures against whatever actually scrolls.

**M19 — one owner decides where a write lands.** The rail's New menu is a
sibling of the folder listing, not a descendant of it, and used to guess: it
hardcoded the space root, so uploading from inside a folder silently landed
at the top of the space. `DriveWriteProvider` (`components/drive-write.tsx`)
is now the sole owner — mounted in `drive.tsx` above the sidebar and the
inset alike, deriving the target from `?dir=` the same way `folder-tree.tsx`
already did, so the rail and the listing can no longer disagree. It also owns
both hidden file inputs and the New-folder dialog, which move out of
`drive.tsx` and `$spaceId/index.tsx`; `ui-store`'s `newFolderOpen` flag — the
only way the dialog used to reach across that boundary — is gone.
`folderActions()` (`components/folder-actions.tsx`) is `nodeActions()`'s
counterpart for the folder itself: New folder, Upload files/folder, Paste,
Select all, Sort by, View, Refresh and Folder information, rendered by both
the rail's dropdown and a new background `ContextMenu` on the listing (right-
click the empty state or the gap below the last row). Paste needs a source,
so nodes can be Cut or Copied onto a small `ui-store.clipboard` — deliberately
outside `partialize`, so it never survives a reload — and a folder can never
paste a cut node into itself or its own subtree (`lib/write-target.ts`).

**M20 — a session is a row, not a signature.** The cookie was
`v1.<uuid>.<expiry>.<hmac>`: self-contained, thirty days, and nothing to
revoke. A lost laptop stayed signed in until the token aged out, "sign out"
only cleared the browser's copy, and the single lever that invalidated
anything was rotating `PP_SESSION_KEY` — which signed out everybody at once,
and rotated the WOPI signing key with them, because one secret served both
token classes. The cookie now carries an opaque 256-bit random token and
`sessions` stores only its SHA-256, so a dump of that table yields nothing a
browser can present, and one indexed join answers "who is this" with the
session row and its owner together. The idle deadline is 30 days, slid lazily
— at most one `UPDATE` an hour per session — under an absolute 90-day cap
measured from `created_at`, and a five-minute ticker in `cmd/server` sweeps
what has expired. Everything that was impossible before follows from the row:
`GET /api/v1/users/me/sessions` lists the devices behind a third tab on the
account page, `DELETE …/sessions/{id}` ends one and `DELETE …/sessions` ends
all but the current, `POST /auth/logout` deletes the row and returns
`end_session_url` so the IdP session ends too (`EndSessionURL` existed all
along and had no caller — "sign out" then "sign in" was a silent SSO re-entry
as the previous person), rotating the master key signs nobody out, and
`users.last_seen` finally means last seen: `TouchSession` writes it in the
same statement, where before it was stamped only at login and the People
table's "last active" column was really "last signed in". Cookies carry
`Secure` whenever `PP_EXTERNAL_URL` is `https://`, alongside HttpOnly and
SameSite=Lax, and the server warns at startup when it is not.
**Authentication is a chi middleware** on the `/api/v1` group instead of
thirty hand-written guards that a new route could simply forget to copy;
handlers call `caller(r)`, which cannot be nil, and
`TestEveryAPIRouteIsAuthenticated` walks the router and fails the build if a
route escapes it. The login handshake keeps state, nonce and PKCE verifier in
one `pp_oidc_tx` cookie cleared unconditionally at the top of the callback —
the two cookies it replaces survived every failure path for their full ten
minutes, leaving a replayable state value behind — verifies the nonce in
constant time, and reports failure by redirecting to `/login?error=<code>`
rather than answering a top-level navigation with plain-text `http.Error`.
And changing the issuer URL no longer 500s every returning user: identity is
still keyed on `(oidc_iss, oidc_sub)`, but a miss now falls back to the
**verified** email and re-points the existing account instead of inserting a
second row that collides with the unique index on `(tenant_id, email_hash)`.

**M21 — deciding once is not deciding.** M20 made a session a row so that it
could be revoked and re-resolved on every request; this pass went looking for
everything else that decided access once and then trusted the answer forever.
A public link checked its own token hash and expiry and nothing else, so
revoking the grant that let somebody share a folder left every link they had
minted working, anonymously, indefinitely — `resolveLinkNode` now re-resolves
the **creator's** `CapShare` on the node and answers 404 once it is gone. A
WOPI `GetFile` accepted rank 1, which made an editor session the way around
the no-download half of a grant: mint a view token, fetch the document, keep
it. `GetFile` now requires rank ≥ 2 and `handleEditSession` requires
`CapDownload`, minting rank 2 even for a read-only session because rank 1
could no longer fetch its own document; the accepted cost is that a reader
with view-but-not-download cannot open a `.docx` at all, only its thumbnail.
`/s/{token}/content` served byte-identical originals of every file in a
subtree regardless of `can_download`, one `?node=` at a time, and
`/preview?kind=text` handed over a verbatim 4 KiB prefix of the source under
view — both take download now. And the caller's own address was the caller's
choice: chi's `RealIP` middleware, deprecated in 5.3.2 for exactly this,
believed `True-Client-IP`, `X-Real-IP` and `X-Forwarded-For` from anybody, so
a rate-limit bucket keyed on it was free to reset and the address on a session
row was whatever an attacker wanted the victim to see. It is replaced by
`middleware.ClientIPFromXFF(PP_TRUSTED_PROXIES…)`, installed only when that
variable names a proxy and read everywhere through `audit.ClientIP`, where an
empty result means "believe the socket". Two controls did not exist at all.
**Same-origin is checked on every mutation**: `Server.sameOrigin` wraps the
`/api/v1` group and `/auth/logout` and runs _before_ authentication, so a
forged cross-site write is refused without a session lookup and with a reason
instead of a 401; it compares `Origin` — falling back to the origin of
`Referer`, never its path — against `PP_EXTERNAL_URL` and fails closed when
that is unparseable, and `/wopi/*` is exempt by design, because the Document
Server sends no Origin and is authorized by a signed token.
`TestEveryAPIMutationChecksOrigin` and `TestWopiIsExemptFromOriginChecks` hold
both halves at the router. **Rate limits exist**: per-IP token buckets on
`/auth/login`, `/auth/callback` and `/auth/logout` (30/min, burst 10),
`/s/{token}` (60/min, burst 20) and `/api/v1` (1200/min, burst 200) — in
process, so N replicas allow N× the rate, which is stated rather than hidden.
Uploads have a ceiling, `PP_MAX_UPLOAD_BYTES` (default 5G) enforced by
`http.MaxBytesReader`: `ParseMultipartForm(32<<20)`'s argument was never a
limit — it is the in-memory spill threshold — and quota is checked only after
the bytes have landed, from a default of unlimited. All of it is finally
observable: a `security_events` table and `internal/audit` record sign-ins and
their failures, logout, session revocations, identity adoption, admin and
suspension changes, grants, share revocation and membership changes with the
client IP, user agent and chi request id, read by
`GET /api/v1/admin/security-events` and purged past
`PP_AUDIT_RETENTION_DAYS` (365) by the existing five-minute sweep.
`PP_BOOTSTRAP_ADMIN_EMAIL` makes the first admin creatable without a psql
session — promoted on every sign-in, so it also recovers an install whose last
admin was demoted, and gated on a **verified** email claim, since otherwise it
would be a standing authorization rule keyed on a field an IdP may let its
users edit. At the edge, nginx now sends CSP, HSTS, `X-Frame-Options: DENY`,
`Referrer-Policy` and `nosniff`, keeps share tokens out of its access log the
way `redactingLogger` keeps them out of the server's, and no longer proxies
`/wopi/` at all; compose splits one flat network into `edge`/`data`/`office`,
so the Document Server can no longer open a connection to Postgres.

## Architecture

System design, component interactions, data flows, deployment, and external integrations.

| File                                                            | Description                                                                                                       |
| --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| [system-design.md](architecture/system-design.md)               | Modular monolith rationale, component map, upload/WOPI/share data flows, tenancy & scale posture                  |
| [data-model.md](architecture/data-model.md)                     | Postgres schema, invariants, query patterns, sessions, the security log, quota handling                           |
| [storage.md](architecture/storage.md)                           | Content-addressed blob store, blake3, local + S3 backends, dedup/versioning, GC                                   |
| [office-integration.md](architecture/office-integration.md)     | WOPI host endpoints, access tokens, OnlyOffice co-editing, locking                                                |
| [permissions.md](architecture/permissions.md)                   | Ownership vs grants, Admin/Everyone groups, capabilities, who may fetch bytes, archive on departure               |
| [preview-pipeline.md](architecture/preview-pipeline.md)         | Preview job flow, job vs preview kinds, the optional worker, caching, backfill                                    |
| [milestones.md](architecture/milestones.md)                     | M1–M21 delivery history **and** the M22–M31 roadmap (Phase A / Continuous / B / C), testing strategy, rollout/ops |
| [production-readiness.md](architecture/production-readiness.md) | P0 rollout blockers, P1 gaps vs Google Drive, the `file:line` evidence under each roadmap milestone, phasing      |

## Codebase

Directory structure, entry points, API patterns, and key modules.

| File                                                      | Description                                                             |
| --------------------------------------------------------- | ----------------------------------------------------------------------- |
| [directory-structure.md](codebase/directory-structure.md) | Monorepo layout, `apps/server` module map, web data layer, entry points |

## Code Standard

Conventions, naming rules, tech stack versions, and development workflows.

| File                                                             | Description                                                                     |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| [backend-conventions.md](code-standard/backend-conventions.md)   | Go stack (chi, sqlc/pgx, go-oidc), module boundaries, error taxonomy, job rules |
| [frontend-conventions.md](code-standard/frontend-conventions.md) | Vite+/bun workflow, api/queries/ui-store layering, catalog versions, style      |

## Project PDR

Product goals, use cases, business rules, and constraints.

| File                                             | Description                                                                             |
| ------------------------------------------------ | --------------------------------------------------------------------------------------- |
| [product-goals.md](project-pdr/product-goals.md) | Vision, competitive landscape, MVP scope, non-goals, deployment model, success criteria |
| [use-cases.md](project-pdr/use-cases.md)         | UC-1..UC-10: drive, sharing, office co-edit, permissions, previews                      |

## Other

Repository-specific docs outside the standard topic folders.

| File                           | Description                                                                                                                            |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| [deployment.md](deployment.md) | Prod + dev compose, env convention, the same-origin constraint, security headers, network segments, Dokploy, `/s/` routing, known gaps |
