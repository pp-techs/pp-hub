# Documentation Summary

pp-hub — a minimal, self-hosted Google Workspace alternative: Drive, collaborative office (OnlyOffice/WOPI), permissions, and multiple spaces.
Monorepo: bun workspaces + Vite+ frontend (`apps/web`), Go modular-monolith backend (`apps/server`), shared UI package (`packages/ui`), Postgres + content-addressed disk storage, compose variants in `deploy/compose/`.

## Agent Context Guide

Before planning or implementing, read this `docs/SUMMARY.md` file first. Load only the detail docs relevant to the current task, and prioritize `Code Standard` docs for implementation conventions. If docs conflict with code or user intent, use the available question tool before making broad changes.

**There is a roadmap — read it before inventing one.** `## Upcoming — M28 Onward` in [milestones.md](architecture/milestones.md) names what is still open — M28 — and the phase each belongs to, and [production-readiness.md](architecture/production-readiness.md) holds the evidence behind them, down to the `file:line`. A request that sounds like a new feature is usually a milestone that already exists with a decided shape and a stated reason for its position; the gaps deliberately left without a milestone are named there too, so "nobody has written this down" is worth checking before it is assumed. The history behind any shipped milestone is one file per milestone under `docs/architecture/milestones/`.

Status: M1–M27 and M29–M50 done (M12–M14 never existed), M28 open — release v0.13.0 — full MVP plus a pilot-ready operational layer: OIDC auth (Logto — PKCE and nonce on the handshake, sessions as rows so a device can be signed out on its own), **space membership projected from a Logto organization** (M33: the organization is the source of truth for who belongs to a company, `space_members` is written only by the reconciler, admin status is the organization's `admin` role, and there is no local invitation and no invitation token — an invitation is a Logto organization invitation sent by Logto and accepted by pp-hub at the invitee’s first sign-in with the verified address (M38); outgoing mail is configured in Admin › Settings and carries share notifications and security alerts (M31, M35). Logto is the only identity provider and is required at startup: spaces are created, renamed and linked through it, and accounts — suspension, deletion, email, name — are its too (M38)), optional **personal spaces and sharing by email across spaces** (M42), drive (spaces/nodes/CAS blob/upload/download/versioning), sharing (direct + link), OnlyOffice WOPI editing with locks that a REST version upload also honours (M24), previews via background jobs — images, PDFs, video and office documents all get a real 256px thumbnail, text gets a 4KB snippet. PDF/video rasterization needs poppler/ffmpeg, and office documents convert through OnlyOffice first; all three live in an **optional** `cmd/worker` container; without it those jobs stay pending rather than failing, and `worker --backfill` sweeps files that predate it. Full-size viewing still streams the original bytes from `GET /api/v1/nodes/{id}/content`.

**M50 — publishing HTML as a public site.** An `.html` file or a folder becomes a mini web site at a stable URL, scripts included — an AI chat's "artifact" link for the team's own drive. **Publish…** in a node's actions (or `publish_artifact` over MCP, which writes the files into a folder of its own first) takes a snapshot: `publication_files` rows naming blobs already in the CAS, counted in `blobs.ref_count` and `used_bytes` by trigger (migration `0030`); Republish replaces them and the URL stays. The bytes are never served as an app page: the same `server` answers them from a separate artifact router — by default under `/a/` on `PP_EXTERNAL_URL` (picked by path prefix before any app header, session or route), or, with the optional `PP_ARTIFACT_URL`, on a second origin picked by `Host` — with nothing else on it and a `sandbox` CSP with `connect-src 'none'` on every response; `/p/{token}` on the main origin is the viewer (bar, optional password, iframe). On the app's origin the sandbox header alone guards the session, so a separate origin is the recommended hardening (`publishing_isolated` on `/users/me`; the admin page warns without it). Publishing needs share and download on the source, the space's `allow_public_links` and the instance's `artifacts_enabled` (Admin › Settings › Publishing, off by default); every request re-checks expiry, the switches and the creator's membership, answering 404. See [publishing.md](architecture/publishing.md) and [milestones/M50.md](architecture/milestones/M50.md).

**M49 — guests open a share with an emailed code, no account.** With `guest_access` on (Admin › Settings › Sharing, off by default; it also needs `external_sharing_enabled` and working SMTP), an address that was shared a **view** invitation, or that already holds a guest grant, can open it from `/open/{id}`: the sign-in page asks for the address, mails a six-digit code (ten minutes, five tries, three mails an hour per address, one answer whether or not a code was sent) and a correct code starts a 7-day sliding session for a `users` row of `kind = 'guest'` (migration `0029`). A guest holds M42's external direct grant at view level, so the resolver is unchanged, and the `authenticated` middleware confines the session to an allowlist of read-and-comment routes (`guest_routes.go`; everything else 403); they get no personal space, are absent from member lists and the admin console, and are promoted in place by a later verified Logto sign-in. See [guest-access.md](architecture/guest-access.md) and [milestones/M49.md](architecture/milestones/M49.md).

**M48 — open files in the drive shell.** A document, sheet, board or PDF renders at `/drive/$spaceId/file/$nodeId` (`/drive/shared/file/$nodeId` from outside a space) under the same rail, through `openFile` (`lib/open-file.ts`). There is one header: sidebar toggle and where you are on the left, and on the right search, notifications and the file's own controls (outline, comments, Share, Versions, Download). There is no in-app tab strip; Account → Preferences → Opening files chooses whether a file opens in the current browser tab (the default) or a new one. `/edit/$nodeId` — the href stored page links use — redirects through `/open/$nodeId`. See [M48](architecture/milestones/M48.md) and [Open files](code-standard/frontend-conventions.md#open-files).

**View reads now; download saves.** The capability split was redrawn: `CapView`
opens the file — `/content` (the inline stream the PDF viewer, `<img>` and
`<video>` read), `/preview?kind=text`, a block document's `/document` and its
`/attachments/{hash}`, comments on a document, and content search — and
`can_download` keeps only the routes that hand a copy over deliberately:
`/download`, the folder zip, `/export`, copy-as-source, and `/collab`. The
M21 rule that `/content` takes download is superseded; a link or a grant that
cannot download can now actually read what it covers, and the share page draws
a PDF, an image, a video or a text file for an anonymous visitor, with no save
button. The honest limit is stated rather than hidden: a render is still the
file's bytes, so saving is not _offered_, not impossible. Share tokens also
shrank from 43 characters to **16 base62 characters** (`newLinkToken`, with
rejection sampling so the alphabet stays even), which is short enough to read
aloud; existing links keep working because resolution is by SHA-256 hash, not
length. The product name is `PP Hub` everywhere (`PRODUCT_NAME`), and the
sign-in page and the header of a shared link share one `BrandMark`.

**M22–M24 closed the P0 register.** There is now a **restorable backup**: `deploy/backup/pp-backup.sh` and `pp-restore.sh` capture Postgres and the blob volume together and consistently, and a restore has actually been performed against a built `compose.prod.yml` stack, not merely scripted. There was also a **mail transport for invitations** — `internal/mail` over SMTP — which **M33 deleted** (invitation email is Logto's job now) and **M31 reinstated** for share notifications, with **M35** moving its configuration into Admin › Settings. Comments and in-app notifications followed in **M29**. And a **REST version upload now honours `file_locks`**: `Server.lockedByEditor` answers 409 rather than silently discarding whatever a colleague has open in the editor (since M30 a replacement is a tus upload, refused with 423 at creation and again at commit). Alongside them, `/healthz` gained a real `/readyz` split that checks Postgres and the blob store, `cmd/worker` gained a health signal, and avatar upload gained an actual blob store behind it.

**M11 — a space is a company.** One instance hosts several companies: space membership grants **no file access at all**, and access comes from `groups` granted on folders (`shares.kind = 'group'`), inherited down the subtree. Resolution is one batched recursive query that also walks through trashed ancestors, so the trash needs no weaker rule of its own. Listings, search, starred, recent, activity and trash all narrow to what the caller can reach, in one batched query per page rather than a walk per row, and `Shared with me` is an actual list of grants rather than a slice of the filing cabinet. Groups are managed from the drawer on the People page in space settings, and a folder's `Who can open this…` dialog is the one place access is granted or revoked; behind them sit `/spaces/{id}/groups` and `/nodes/{id}/grants`. A direct share may not name someone outside the space — that is what link shares are for.

**M15 — permissions are owner-anchored.** Access comes from exactly two things, both gated on **current membership of the node's space**: **ownership** (`nodes.created_by`, inherited down the subtree and resolved in the same recursive query as the grants, so it is not a revocable row and nobody can be locked out of their own work) and **grants**. Everything a person creates is private to them until they share it — and leaving the space ends their reach, so a departure files their work in the Archive instead of moving a folder they can still read. The role scale is gone — no `space_members.role`, no `default_role`, no `roleRank`, no `RequireRole` — and **admin no longer means access**: admin is membership in the space's automatic `Admin` group, it gates space administration via `spaces.RequireAdmin`, and it gates no file at all, because the old model handed every admin an edit floor over the whole company tree. `shares.perm` became `level` (`view` | `edit`) plus independent `can_download` and `can_share` flags — the wire carries that triple directly, since no single ranked word can say "view and pass on, but do not download" — with DB CHECKs keeping an editor able to download and links view-only and unchainable, and `Caps.Covers` refusing to hand out more than the sharer holds (403). `can_download` gates taking a copy, not seeing bytes: `/content` streams inline under view, `/download` needs the flag, for members and anonymous links alike. **Superseded by M21**, which found that handing over the complete file is a download whatever the `Content-Disposition` says — `/content` needs the flag too now, on both surfaces. Departure has an answer instead of a disclaimer: every space owns an `Archive` folder whose only grant is edit+download+share to the `Admin` group, and `RemoveMember` revokes the leaver's grants both ways, climbs nested ownership to the nearest ancestor owner, moves their remaining roots into the Archive and drops their group rows, refusing to lose the last admin (409). Personal folders retired — `personal_folders`, `spaces.personal_root_id` and the pinned "My files" are gone, a member's top level is `ListEntryPoints` (what they own or were granted), and a member can finally create a folder at their own top level. Node DTOs carry `caps`, so the UI offers exactly what the caller holds: sharing and the `Who can open this…` dialog follow `caps.share` rather than admin status, and any member may read the space's group list in order to grant one access. The fifteen development migrations were squashed into one baseline, `0001_init.sql`. See [permissions.md](architecture/permissions.md).

**M16 — login → account → space has one shape.** _(Settings surfaces superseded by M34.)_ Every drive surface is a child route of `/drive/$spaceId/`: the listing plus `home`, `recent`, `starred`, `shared` and `trash`. Before this, Home, Recent and Starred were siblings of `$spaceId`, so opening one emptied the rail — the switcher fell back to "Select a space", the folder tree disappeared and the Trash and Shared rows, which were `?panel=` values needing a space, vanished with it. `?panel=` is gone entirely, along with `type DrivePanel`. Space membership got exactly one home: one table carrying the Admin switch, group chips and — since M33 — pending departures as rows, replacing a `?panel=members` panel and a Members tab that rendered the same component at two addresses while the Groups tab insisted admin membership was "managed in the Members tab". Because admin _is_ membership of the automatic `Admin` group, membership mutations invalidate `["spaces", id]` rather than just the member list. A settings pane is a place the router owns, and a non-admin is refused rather than shown a "you cannot" pane at a URL that resolved; the standalone `/profile` page moved inside the shell. `recent`, `starred` and `shared` gained the optional `?space=` that `activity` and `usage` already had, filtered in SQL because all three are limited and a post-filter would return a short page. Deep links survive sign-in: `?redirect=` on `/login` and a short-lived, revalidated `pp_oidc_next` cookie across the OIDC round trip, accepting same-origin paths only. Cross-space Home was removed rather than deferred — a row from another space cannot be opened without silently switching the space the rail is showing.

**M17 — one dialog owns access.** `Share` on any node — file or folder — is the single place groups, people and the public link are granted and revoked; the folder-only `Who can open this…` dialog and the email-only share panel are gone, along with `?panel=share` (`PanelKind` is now just `versions`). Grantees are picked from the space, never typed: one combobox offers the space's groups above its members, filtered locally because `GET /spaces/{id}/groups` and `GET /spaces/{id}/members` are both member-readable and already cached, with one level/download/re-share control applied to everyone picked. Group grants now reach files too — the server always allowed it (`Grant` only checks that node and group share a space, and a share row on a file is an entry point like any other), the client merely hid it; the same widening fixed a share list that rendered a group grant as a row labelled with the literal word "group". `GET /users/search` was rescoped here from an unscoped address book across every company on the instance to accounts sharing a space with the caller — and then removed entirely in M33, along with the invite path that was its only reason to exist.

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
counterpart for the folder itself: New folder, Upload files/folder, the
office and markdown creation block — its own `documents` group, so the menus
draw it apart from the folders and uploads, and each entry wearing the
iconify `vscode-icons` glyph its created file will carry in the listing —
Paste, Select all, Sort by, View, Refresh and Folder information — Space
information at the root, where the details panel describes the space rather
than a folder — rendered by both the rail's dropdown and a new background
`ContextMenu` on the listing (right-click the empty state or the gap below
the last row). Paste needs a source,
so nodes can be Cut or Copied onto a small `ui-store.clipboard` — deliberately
outside `partialize`, so it never survives a reload — and a folder can never
paste a cut node into itself or its own subtree (`lib/write-target.ts`).

**The space root is a subject, not a hole.** A space root has no node row —
top-level nodes are `parent_id IS NULL`, a set rather than a child of
anything — so `(i)` there used to answer "Select a file or folder to see its
details" and the background menu dropped its information entry entirely.
`DetailsPanel` now takes a `DetailsSubject` (`{ kind: "nodes" }` or
`{ kind: "space" }`) and renders a space sheet at the root: name,
description, storage against quota, what the listing is showing, the member
count and your access, with Space settings / People for an admin. Nothing is
fabricated and nothing new is fetched but the member list. The `My Drive`
breadcrumb was inert for the same reason — it was built without a `dirId`
and only a crumb with one rendered as a link — so from inside a folder there
was no way back to the top of the space through the header; every crumb but
the last is a link now, the root's clearing `?dir=`.

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
the bytes have landed, from a default of unlimited. (Since M30 uploads are
tus: the ceiling is checked against the declared `Upload-Length` at creation
and bounds every chunk, and `MaxBytesReader` via `limitBody` remains only for
WOPI `PutFile` and the avatar.) All of it is finally
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
way `redactURI` keeps them out of the server's, and no longer proxies
`/wopi/` at all; compose splits one flat network into `edge`/`data`/`office`,
so the Document Server can no longer open a connection to Postgres.

**M39 — block documents.** A `.md` file now opens in a block editor (slash-editor) at `/edit/$nodeId`, co-edited live over Yjs with presence avatars and remote carets — see [block-documents.md](architecture/block-documents.md). The sync server is reearth/ygo inside `apps/server` (`internal/collab`), vendored with three patches under `third_party/ygo`, rather than a Hocuspocus sidecar: one binary stays one binary. A room (`<nodeId>.<epoch>`, from `GET /api/v1/nodes/{id}/collab`) is seeded from the current version by `internal/mdoc`, which golden fixtures generated from slash-editor itself hold to its serializer, and is written back as an ordinary version authored by its writer after 30 s quiet (5 min at most) and on shutdown, only when the markdown changed. While resident it holds a `collab:` file lock, and every 60 s it re-resolves each peer's session and capabilities. Images and files dropped in are `node_attachments` rows owned by the document, uploaded by tus with `attach_to` and referenced as `attachment:<hash>`. Markdown outside the block set opens read-only as lossy; `0014` backfilled `text/markdown` onto older `.md` versions so they open at all.

**M40 — block documents, completed.** What M39 left a document without: `internal/blockdoc` reads a version as the ProseMirror tree (`GET /nodes/{id}/document[?version=]`, `CapDownload`), rendered read-only by one web component, `components/markdown-document-view`, in the preview panel, the version preview (with Restore) and the share page (`/s/{token}/document` and `/attachments/{hash}`, the link's download terms — a link renders, never edits). `GET /nodes/{id}/export` zips the markdown with its attachments rewritten to `assets/`. Five templates in English and Vietnamese (`GET /templates/markdown`, `"template"` on document creation) are canonical `mdoc.Render` output, so they open editable and unrewritten. The block set gains `video`, `embed` bookmarks — fetched by the server through `internal/unfurl`, which checks every dialled address (redirects and DNS rebinding included) against private, loopback, link-local and metadata ranges and stores the preview image as an attachment, never hotlinked — `mention`, and the `comment` mark, which slash-editor drops and `internal/mdoc` alone persists as an invisible HTML-comment marker pair. Write-back adopts attachments pasted from another document the writer can read, and stale collab locks are cleared only after the listener binds (`listenThenStart`). Web: table controls, outline panel, @mentions, presence colours and initials, narrow-screen layout. See [milestones/M40.md](architecture/milestones/M40.md).

**M47 — WebDAV access.** A person mounts their drive at `${PP_EXTERNAL_URL}/dav/` in Finder, Explorer or rclone (`--vfs-cache-mode full`, recommended for agents) so a coding agent on their machine can run local tools over the company's files — see [webdav.md](architecture/webdav.md) and [milestones/M47.md](architecture/milestones/M47.md). WebDAV rather than a sync client: nothing is copied, so nothing drifts. Every write is an `/api/v1` call answered in process by the agent router as the token's owner (`innerRequest`), so capabilities, locks, quota, activity and audit are the web client's; a save over a file is a new version, a raced write lands as a conflicted copy, safe-save patterns keep the original node, `DELETE` trashes, editor scratch files never become nodes. It signs in with a personal token of `purpose = 'dav'` (migration `0025`; Basic password or Bearer; refused at `/mcp/`, audit `dav:<id>`), minted in Settings › Security › Mount as a drive. `webdav_enabled` (Admin › Settings › WebDAV) is **off** by default; the server answers `/dav` with no body cap and no buffering, and a proxy in front must allow the same.

**M44–M46 — apps (extensions) and the kanban board.** pp-hub carries small apps compiled into the server and the SPA behind `apps/server/extsdk` — see [extensions.md](architecture/extensions.md) and [milestones/M44.md](architecture/milestones/M44.md). An app ships **off**: an instance administrator switches it on (Admin › Apps), a space administrator can switch it off for a space (Settings › Apps). It owns a node kind (`nodes.kind = 'ext'`, `ext_kind`), a Postgres schema `ext_<id>` with its own migration track, and routes under `/api/v1/ext/<id>` inside the authenticated group; access is always `share.EffectiveCapsForNode` of the node, never the extension's own. `TestExtImportsOnlySDK` keeps extensions off `internal/`. The first app is the kanban board ([M45](architecture/milestones/M45.md), [M46](architecture/milestones/M46.md)): a board is an item in a folder (shared, starred, copied, trashed like a file, no bytes), cards move by fractional-index positions with `rev`-checked edits and live SSE updates, assignees must be able to open the board, a card holds links to drive files that are re-resolved per viewer (a name never reaches someone without access) and a discussion — comment threads with a `subject` of `card:<id>`, @mentions, and an `ext:kanban:assigned` notification. No sandbox: including an app in a build is trusting its code.

**M43 — pages.** A block document can hold sub-pages, Notion-style — see [milestones/M43.md](architecture/milestones/M43.md) and [block-documents.md](architecture/block-documents.md#pages). A sub-page is a `.md` node whose `parent_id` is its parent document, so the page tree is the node tree: one rule, `fs.CanContain` (a folder holds anything, a document only documents; `ErrNotContainer` → 400), guards create, move, copy, restore and upload, and sharing, trash and purge follow the subtree with no new code. slash-editor 0.7.0's `subPage` block and `pageLink` chip are stored as its own markers; `mdoc.RenderWith` writes live titles looked up at write-back, `doc:` mentions read as page links, and a page's `icon` and `cover` are frontmatter keys, live in the room as a `Y.Map` named `meta`. The `extract:text` job also projects `page_meta` and `page_refs` (`0021_pages`), which back `GET /nodes/{id}/page`, `/pages` and `/backlinks` (404 for anything the caller cannot view) and `child_page_count` on listings. Write-back now writes only after an attributed edit, so a renamed linked page reaches the file with the next real edit and never as a version nobody made. Share links browse sub-pages (`/s/{token}/page`, `/pages`); export and the folder zip lay pages out as `<Doc>.md` + `<Doc>/<Sub>.md` with relative links; copy remaps page ids across the copied tree in one transaction; MCP and search follow parent documents. Node restore now answers 200 `{node, relocated, renamed_from?}`. Not built: databases, drag re-parenting, realtime title push, non-markdown sub-pages.

**M42 — personal spaces and external sharing.** One account, many spaces: when `personal_spaces` (Admin › Settings › Personal use: `off` | `members` | `everyone`, default `off`) admits an account, sign-in (`auth.SignInHook` → `internal/personal`) provisions a personal space — `spaces.kind = 'personal'`, no Logto organization, owner as only member and admin, no Archive, quota from `default_personal_quota_bytes` (required above 0 for `everyone`) — listed first and labelled "My Drive". It refuses administration it does not have (409: members, groups, departures, team folders, linking, deletion). Sharing crosses spaces through **external grants**: `POST /nodes/{id}/shares` with `email` gives a member an ordinary grant, an account outside the space a direct share flagged `is_external`, and an address with no account a `pending_shares` invitation (202 + mail) adopted at its first verified sign-in. `NodeAccessCaps` honours, for a non-member, only live external direct shares while `external_sharing_enabled` and the space's `external_sharing` hold — never group grants, never ownership. Outsiders browse by node (`/shared?scope=external`, `/nodes/{id}/children|path|people`; web `/drive/shared`, mails link to `/open/{id}`); 50 new outsiders per sender per day; agents need `mcp_external_shares`. Migration `0019`. See [personal-spaces.md](architecture/personal-spaces.md).

**M41 — agents over MCP.** Claude.ai, Claude Desktop and ChatGPT act as the signed-in person through a remote MCP server at `${PP_EXTERNAL_URL}/mcp/` (`internal/httpapi/mcp.go`, official go-sdk, stateless) — see [agent-access.md](architecture/agent-access.md). A chat app has no shell, so MCP rather than a skill + CLI. Logto is the authorization server: clients sign in with a Client ID Metadata Document, which `pp-logto-setup.sh` enables along with the API resource `${PP_EXTERNAL_URL}/mcp/`; pp-hub only verifies the `at+jwt` (issuer, exact audience, expiry) and resolves the account by `(oidc_iss, oidc_sub)` — no Logto scopes or roles, confirmed live with an empty `scope`. Clients configured from a file (`.mcp.json`), CI and scripts use a **personal access token** instead (`pph_…`, `agent_tokens` `0018`, hash only, 1–90 days, minted and revoked in Settings › Security and never through an agent; `mcp_tokens` turns them off). Every request re-checks `mcp_enabled` (Admin › Settings, off by default), suspension, the person's `agents_enabled` (Settings › Security, which also shows the connector URL) and a per-person rate limit. The 25 tools in `internal/agenttools` are REST calls that `agentAPI` answers in process through `apiRoutes` — the web client's own handlers, so access, audit and notifications are exactly a person's; `create_public_link` is offered only under `mcp_public_links`. Two agent-only routes read and write a document's text (`/nodes/{id}/text`, view/edit, through `collab.Flush`/`Reseed`, so a stale etag is a 409 and an accepted write reaches the room live), and `add_comment` anchors threads via `mdoc`. Security events from an agent carry `via: "mcp"` and the `client_id` (`pat:<id>` for a personal token). See [milestones/M41.md](architecture/milestones/M41.md).

**M29 — comments and notifications.** Comments exist on block documents only: a thread is anchored by its id in the markdown's comment marker and stored in `comment_threads`/`comments` (`0015`), so it survives reloads and restores. Taking part takes `CapDownload` — whoever can open the document can discuss it. `internal/notifications` writes a row per person told (document owner for a thread, participants for a reply, anyone @mentioned — in a comment, or in the document text via the `notify:mentions` job on the version hook) and pushes it over Server-Sent Events (`/api/v1/notifications/stream`, served unbuffered) to a bell in the header. The broker is per process, so one `server` instance. In-app only: no comment email. See [milestones/M29.md](architecture/milestones/M29.md).

**M27 — content search.** Markdown (via `mdoc.PlainText`), plain-text, PDF and office files are indexed into `node_texts` (`0016`), folded so Vietnamese matches without accents, backfilled by `cmd/server` at startup; `GET /search` ranks name hits above content hits and returns `match`, `snippet` and `snippet_highlights`, which the palette highlights. Markdown and plain text are extracted by `extract:text` in `cmd/server`; PDFs by `extract:pdf` (`pdftotext`) and office documents by `extract:office` (OnlyOffice converts to PDF, then `pdftotext`) in `cmd/worker`, so until a worker runs those files stay matched by name only. Scans are not OCR'd. See [preview-pipeline.md](architecture/preview-pipeline.md#text-extraction).

**M34 — settings is one dialog, addressed by the hash.** M16 gave settings an address and, with it, two of everything: three account routes behind one tab strip, three space routes behind a second copy of it, entered from two different rail menus, each taking over the whole content column. There is now a single dialog opened by `#settings/<pane>` on whatever page you are on — `general|profile|security` about the account, `space|members|groups|danger` about the space — so flipping a theme no longer navigates away from the folder being worked in. The invariant M16 paid for is untouched: the router still owns which pane is open. `settings-hash-bridge.tsx` is the only caller of `SettingsModal.show/hide` and it reads the hash, so the flow is one-way and every exit inside the dialog is a navigation; opening pushes and switching pane replaces, which is why Back closes the dialog instead of walking out through each pane. Space panes are gated by filtering the band out for a non-admin and replacing a deep link to one of them, rather than rendering a refusal at an address that resolved. The dialog's nav is plain markup, because `ModalContainer` lives outside the shell's `SidebarProvider` and a second provider would register a second ⌘B handler; unsaved edits block Escape and backdrop clicks, which a page never needed because leaving one took a deliberate navigation. Membership split honestly along the way: Members and Groups are separate panes, the group filter is component state, and `?group=` is gone. Ten route files and `settings-tabs.tsx` were deleted rather than redirected.

**M38 — two-way Logto sync.** Logto is now mandatory and the only identity provider: `config.LogtoSettings.Validate()` refuses to start the server without `PP_LOGTO_ENDPOINT` and the M2M credentials, and the claims-only and no-tenant modes are gone. Every space-level write goes upstream first — an instance admin creating a space creates the Logto organization and is seated in it as admin (`orgsync.CreateSpace`), a rename PATCHes the organization, and each reconcile mirrors the organization's name back. Deletion travels in neither direction: an organization deleted in Logto unlinks its space and keeps its members and content, and a space deleted here leaves its organization in Logto behind a `detached_orgs` tombstone (`0012`) so the next sign-in does not recreate it. A space that mirrors nothing is linked by an instance admin with **Link to Logto** (`link-preview`, then `POST /spaces/{id}/link` and an `org:link` job that pushes the members before writing `idp_org_id`). Invitations are accepted at sign-in, and suspension, deletion, email and name are converged from Logto by webhook and hourly sweep. An empty `organizations` claim means "no organizations"; an absent one is skipped with a warning. See [M38](architecture/milestones/M38.md).

**M37 — blob integrity.** A review of the local store found GC could collect a blob an upload had just deduplicated against, leaving the new file pointing at nothing (reproduced). Every `blob.Store` now takes `blob.Claims` and claims a pinned `blobs` row (`ClaimBlob`) before it installs or deduplicates bytes; GC deletes row and bytes in one transaction under the row lock, with grace counted from `released_at`. `ref_count` is maintained by triggers on `node_versions`, `previews` and `users.avatar_blob` (migration `0011`, cascades included, previews counted for the first time) — no Go code moves it. Blobs and their directories are fsynced before the reference commits; a same-named copy of the wrong length is replaced rather than reused; `Ingest` survives `.uploads` on another filesystem. `gc.Worker` runs a daily audit (tidy temp files, recount, adopt bytes without a row) and an hourly scrub (`PP_BLOB_SCRUB_BYTES_PER_DAY`, `pphub_blobs_damaged`); disk free is `pphub_blob_disk_free_bytes`. See [storage.md](architecture/storage.md#garbage-collection).

**M36 — sharing hardening.** Moves can no longer widen the mover's own access or take personal work into a team folder behind its creator's back (`authorizeMove`, also applied by `move-impact`); changing or revoking a grant requires covering it, and a team folder keeps at least one manager (409). Direct grants are one row per (node, person) (`0009`), re-sharing upserts, and `POST /nodes/{id}/shares` takes `user_id` instead of `email`. Links lose download when their creator does; the public page never shows the sharer's email. "Shared with me" is membership-gated, admins cannot add themselves to a group that opens files, group changes are audited, multi-row writes run in transactions (`store.Queries.InTx`), and `GET /nodes/{id}/inherited-access` lets the share dialog show everyone who reaches a node. See [permissions.md](architecture/permissions.md).

**M30 — resumable upload and honest accounting.** The multipart upload endpoints (`POST /spaces/{id}/files`, `POST /nodes/{id}/versions`) are gone; every browser upload is tus 1.0.0 at `/api/v1/uploads` (`internal/httpapi/uploads.go`, `internal/uploads`, migration `0010`), so each request is sized to finish well inside a proxy's 60 s read timeout and stays under Cloudflare's 100 MB body cap — the incident in the Dokploy section of [deployment.md](deployment.md) that made it urgent. Creation checks the upload cap and the quota (plus bytes other in-flight uploads have promised) against the declared length; an interrupted chunk keeps what arrived; the final PATCH commits before it answers; a replacement names `node_id` and is refused with 423 while the file is open in the editor. Staging lives in `<PP_BLOB_ROOT>/.uploads` (persisted, excluded from backups), idle uploads expire after 24 h, and the upload routes assume one `server` process. The web client (`lib/api/tus.ts`) adapts chunk size to throughput and retries proxy failures, so the tray's pause, resume and retry act on the transfer in flight. Accounting now counts **every stored version, trash included**, a new version costs its full size, and `version_retention_count` (Admin › Settings, default 100) prunes history on each save; the storage breakdown adds "Older versions" and "Trash". See M30 in [milestones.md](architecture/milestones.md).

**M31 — document creation, and share notifications.** `POST /api/v1/spaces/{id}/documents` writes a blank document, spreadsheet or presentation from `internal/office/templates.go` into the write target M19 resolves, in the caller's locale, and opens the editor on it — before this, every document had to be made elsewhere and uploaded. The same delivery reinstated mail for **share notifications**: `internal/notify.Shared` enqueues one message per recipient as a job, honoring a per-user opt-out (`users.settings.email_on_share`) and rendering in the recipient's locale. M29 is therefore comments plus in-app notifications, not "no mailer at all".

**M35 — instance settings in the admin console.** Operational policy lives in `system_config` (JSONB, `internal/sysconfig`) with environment-variable fallbacks and per-key validation: `instance_name`, `max_upload_bytes`, `audit_retention_days`, `read_audit_retention_days`, the alert thresholds, `default_space_quota_bytes`, `version_retention_count` and `smtp` (password encrypted). Administrators change them at `/admin/settings` without a restart, test the transport with `POST /admin/settings/smtp/test`, and every change writes a `settings_updated` security event.

**M26 — audit completeness.** `security_events` now answers "who read this": `file_downloaded`, `folder_downloaded`, `file_viewed` and `file_opened` are recorded where bytes are served (signed-in users and share-link visitors), deduplicated in memory to one row per (kind, actor, node) per hour, keyed on a new indexed `node_id` with no FK so a purge cannot erase the record, and purged on their own `read_audit_retention_days` (90). The console filters by kind, file/folder subtree and date range, and `GET /api/v1/admin/security-events/export` streams the same filters as CSV (BOM, formula-injection guard, keyset paging; the export is itself audited). `internal/alert` scans every minute for failed sign-in bursts per address (`alert_failed_login_threshold`/`_window_minutes`, 10 in 15) and raises a `security_alert` row, `pphub_security_alerts_total{rule}` and an email to every instance admin.

**M25 — observability, endpoints only.** `internal/obs` installs one `slog` handler in both binaries (`PP_LOG_LEVEL`, `PP_LOG_FORMAT`; JSON in `compose.prod.yml`) that stamps chi's request id on every record logged with a request context, and the same id is returned as `X-Request-Id` — so a user report, its access log line and its `security_events` row join on one value. The access log is `httpapi.Server.observe` (redacted path, route pattern, status, duration, client address, `user_id`), and the server exposes Prometheus metrics on a separate internal listener, `PP_METRICS_ADDR` (`:9091`, `off` disables): HTTP by route pattern, Postgres pool, job backlog per kind, referenced vs. awaiting-GC blobs, Go/process. No tracing, no error tracking, no bundled Prometheus/Grafana.

## Architecture

System design, component interactions, data flows, deployment, and external integrations.

| File                                                            | Description                                                                                                                                                                                                                                                                                        |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [system-design.md](architecture/system-design.md)               | Modular monolith rationale, component map, upload/WOPI/share data flows, tenancy & scale posture                                                                                                                                                                                                   |
| [data-model.md](architecture/data-model.md)                     | Postgres schema, invariants (the `fs.CanContain` tree rule), query patterns, sessions, the security log, comments and notifications, `node_texts`, `page_meta`/`page_refs`, quota handling, the dormant `tenant_id`                                                                                |
| [storage.md](architecture/storage.md)                           | Content-addressed blob store, blake3, local + S3 backends, dedup/versioning, claims, trigger-kept ref counts, GC, audit, scrub                                                                                                                                                                     |
| [office-integration.md](architecture/office-integration.md)     | WOPI host endpoints, access tokens, OnlyOffice co-editing, locking                                                                                                                                                                                                                                 |
| [block-documents.md](architecture/block-documents.md)           | M39/M40/M43: `.md` documents in slash-editor, Yjs co-edit via patched reearth/ygo, rooms/epochs/locks, `internal/mdoc` + golden fixtures, attachments, templates, read-only/share rendering, export, unfurl SSRF rules, mentions, emoji, comment anchors, pages (sub-pages, page links, backlinks) |
| [permissions.md](architecture/permissions.md)                   | Ownership vs grants, Admin/Everyone groups, capabilities, who may fetch bytes (documents, export, share views), comments, organization sync, departures                                                                                                                                            |
| [preview-pipeline.md](architecture/preview-pipeline.md)         | Preview job flow, job vs preview kinds, the optional worker, `extract:text`/`extract:pdf`/`extract:office` for content search, caching, backfill                                                                                                                                                   |
| [logto-org-sync.md](architecture/logto-org-sync.md)             | Membership as a projection of a Logto organization: the ownership boundary, the three sync paths, departures                                                                                                                                                                                       |
| [personal-spaces.md](architecture/personal-spaces.md)           | M42: personal spaces (`spaces.kind`), external grants and pending shares by email, `personal_spaces` off/members/everyone, the resolver's non-member path, outsider routes                                                                                                                         |
| [guest-access.md](architecture/guest-access.md)                 | M49: guest users verified by an emailed one-time code, view + comment through M42's external grant, the route allowlist, promotion at the first Logto sign-in                                                                                                                                      |
| [milestones.md](architecture/milestones.md)                     | Index of the M1–M49 delivery history (detail in `architecture/milestones/`) **and** the open roadmap (M28), testing, rollout/ops                                                                                                                                                                   |
| [i18n.md](architecture/i18n.md)                                 | Lingui + gettext `.po`, locale precedence, account preferences, Weblate wiring, OnlyOffice `ui=`                                                                                                                                                                                                   |
| [production-readiness.md](architecture/production-readiness.md) | P0 rollout blockers, P1 gaps vs Google Drive (comments and content search closed), the `file:line` evidence, phasing                                                                                                                                                                               |
| [agent-access.md](architecture/agent-access.md)                 | M41: agents act as the user over a remote MCP server; Logto CIMD as authorization server, bearer-only `/mcp/`, in-process tool dispatch through `apiRoutes`, text routes, comment anchors, switches, audit, risks                                                                                  |
| [webdav.md](architecture/webdav.md)                             | M47: the `/dav` mount — why WebDAV over a sync client, `dav` tokens, namespace and labels, writes through the agent router, conflicted copies, safe saves, scratch files, locks, clients, proxy and switch                                                                                         |
| [publishing.md](architecture/publishing.md)                     | M50: HTML artifacts under `/a/` or on an optional second origin — snapshots, the artifact router and its CSP, the same-origin tradeoff, the `/p/` viewer, passwords as path grants, who may publish, settings, agent tools, risks                                                                  |
| [extension-authoring.md](architecture/extension-authoring.md)   | Writing an app: `pp-app.json`, the Go and web SDKs, the import boundaries, `pp-apps` (new, gen, check, add), compatibility, and what a marketplace would still need                                                                                                                                |
| [extensions.md](architecture/extensions.md)                     | M44–M46: compile-time extensions behind `extsdk`, per-instance/per-space switches, `ext` node kind, schema per extension, the kanban board (positions, rev, SSE, comments, links, assignment notices), rejected sidecar/WASM/`plugin` options                                                      |

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

| File                                             | Description                                                                                                                                                                  |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [product-goals.md](project-pdr/product-goals.md) | Vision, competitive landscape, MVP scope, non-goals, deployment model, success criteria                                                                                      |
| [use-cases.md](project-pdr/use-cases.md)         | UC-1..UC-18: drive, sharing, office co-edit, permissions, previews, block documents, comments, content search, personal spaces, sharing outside a space, pages, guest access |

## Other

Repository-specific docs outside the standard topic folders.

| File                               | Description                                                                                                                                                                                                                                                                  |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [self-hosting.md](self-hosting.md) | Step-by-step install on any Docker host: generate the files, start Logto, `pp-logto-setup.sh`, start pp-hub, first sign-in, day two, troubleshooting                                                                                                                         |
| [deployment.md](deployment.md)     | Prod + dev compose, env convention, the Logto setup checklist and its variables, the same-origin constraint, security headers, collab socket, notification stream and MCP locations, agent access, link-preview egress, network segments, Dokploy, `/s/` routing, known gaps |
