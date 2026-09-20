# Delivery Milestones

Each milestone ends in a working end-to-end state inside the compose stack.

## M1 — Skeleton ✅ done

- `apps/server` boots; chi router; OIDC login against **Logto** (cloud or self-hosted in compose); `GET /me`. **Replaced in M20** by `GET /api/v1/users/me` as the single identity endpoint.
- Website calls the API (same-origin via Vite proxy; OpenAPI-generated TS client deferred until the API surface grows in M2).
- Verified: `go test ./...` (session cookie unit tests), `vp check`, OIDC discovery and login flow.
- IdP note: Logto là IdP (Cloud tenant mặc định cho dev, hoặc self-host qua `--profile idp`); backend dùng standard OIDC. Setup steps live in [deployment.md](../deployment.md) (dev stack: `compose.dev.yml`).

## M2 — Drive Core ✅ done

- Spaces (create/list, member roles owner/editor/viewer), nodes (mkdir/rename/move/delete/list), CAS blob store (blake3, dedup), upload/download.
- Upload same-name = new version (not conflict). Version history list + restore.
- Website drive UI: space sidebar, folder tree + breadcrumbs, upload, download, versions panel.
- Verified: `go test ./...` (auth, blob), E2E via compose — space CRUD, mkdir, upload v1/v2, download, version restore, rename, delete, cross-user permission (403).

## M3 — Sharing ✅ done

- `shares` table (link/direct, perm view/download/edit, optional expiry); sqlc CRUD.
- `share` module: token (sha256) link creation, direct grant by email, revocation, `EffectivePerm` resolver (space role widened by direct shares on node/ancestors; shares never narrow).
- Anonymous routes: `/s/{token}` (info) + `/s/{token}/download` (link-clamped to read-only).
- Website: 🔗 button per file → share dialog (direct by email, link with perm/expiry, list + revoke); `/s/<token>` landing page.
- Verified E2E: dev shares `shared.txt` → alice downloads via direct grant (200) but rename blocked (403); anonymous link info + download work; invalid token → 404.

## M4 — Office ✅ done

- `file_locks` table; `wopi` module (HMAC token sign/verify, Lock/Unlock/Refresh with conflict semantics); `handleEditSession` issues token + editor URL.
- WOPI host endpoints: `GET /wopi/files/{id}` (CheckFileInfo), `GET/POST .../contents` (GetFile/PutFile), `POST .../` (X-WOPI-Override: LOCK/UNLOCK/REFRESH_LOCK/GET_LOCK).
- OnlyOffice Document Server 9.x in compose (`onlyoffice` service, :9980), `WOPI_ENABLED=true`, `JWT_ENABLED=false` for dev. Editor URL is `/hosting/wopi/{type}/edit`; the SPA POSTs `access_token`/`access_token_ttl` via a hidden form into the iframe (9.x dropped the old `index.html?wopiSrc=…&access_token=…` GET shim). `PP_WOPI_SERVER_URL` must be container-reachable (`host.docker.internal` on macOS).
- Website: 📝/Edit on office files → opens `/edit/{nodeId}` in a new tab (full-page, not a modal) → edit-session → POST form → iframe OnlyOffice editor.
- Verified E2E: edit-session → CheckFileInfo (200, canWrite=true) → GetFile → Lock → PutFile → version v2 → download new content; lock conflict 409 with X-WOPI-Lock echo; bad token 401.

## M5 — Previews & Jobs ✅ done

- `jobs` (kind/payload/status/attempts/backoff) + `previews` (version+kind→blob_hash) tables.
- `preview.Worker` claims with `FOR UPDATE SKIP LOCKED`, filtered to the job kinds it registered renderers for. Images (png/jpeg/gif/webp), PDFs and video all render to a 256px PNG thumbnail; text takes the first 4KB. See [preview-pipeline.md](preview-pipeline.md).
- Hook: `fs.SetVersionHook` enqueues a preview job after every `PutFile` commit.
- `GET /api/v1/nodes/{id}/preview?kind=…` serves the preview blob (image/png or text/plain).
- `GET /api/v1/nodes/{id}/content` streams the original bytes inline (allowlisted MIME only,
  Range-capable) — how PDFs and video render at full size, since the generated thumbnail is only 256px.
- Website: 👁 button per file → inline preview (img for images, pre for text).
- Verified E2E: upload PNG → thumbnail 200 (74B PNG); upload text → text preview 200 with content.
- Delivered post-M5: real PDF rasterization (pdftoppm) and video frame extraction (ffmpeg), in an optional `cmd/worker` container. Office thumbnails via the OnlyOffice conversion API remain deferred.
- Expired-lock sweep runs every 5m in-process.

## M6 — Trash, Move & Upload Queue ✅ done

Closes the gaps between the shipped app and the Stitch screens that needed no new tables.

**Repairs** — two handlers existed but no router mentioned them, so the web client's calls 404'd:

- `DELETE /api/v1/nodes/{id}/shares/{shareID}` → `handleRevokeShare` was never registered; the share dialog's revoke button had never worked.
- `handleListShared` ignores its `{spaceID}` and spans every space, so it moved from `/api/v1/spaces/{spaceID}/shared` to **`GET /api/v1/shared`** — the path the client was already calling. **Breaking:** any caller of the old nested path must move.
- `PATCH /api/v1/nodes/{id}/move` and `fs.Move` were implemented and unused. The web app now exposes them as "Move to…" in the node menu and the selection bar, via a lazy folder picker (`move-dialog.tsx`).
- `routes_test.go` walks the chi router and asserts every route the web client calls is registered — the regression that let revoke rot is now a test failure, not a 404.

**Trash lifecycle** — no migration; `deleted_at`, `SoftDeleteNode` and `ListDeleted` were already in place.

- `POST /api/v1/nodes/{id}/restore` → `{node, relocated, renamed_from?}`. Handles a trashed parent (re-parents to the space root) and a name a live sibling has taken (suffixes `name (2)`). See the Trash Lifecycle section of [data-model.md](data-model.md).
- `DELETE /api/v1/nodes/{id}/purge` — hard delete of the subtree, releasing blob `ref_count` once per version.
- `POST /api/v1/spaces/{id}/trash/empty` → `{purged}`.
- `GET /api/v1/spaces/{id}/trash` now returns `location` (original path) and `child_count` alongside the node, so a restore decision does not need a second lookup.
- All three mutations require space **owner** — see the rank-mapping note in [permissions.md](permissions.md).

**Upload queue** — client-side against the existing `POST /spaces/{id}/files`.

- `lib/upload-store.ts`: queue with a concurrency limit of 3, per-file progress, cancel, retry, pause; a promise-keyed mkdir cache so a 400-file folder issues one mkdir per directory, not per file.
- `lib/drop-items.ts`: flattens a drop through `webkitGetAsEntry`, draining `readEntries` in a loop (it returns at most 100 per call), and a folder pick through `webkitRelativePath`.
- `transfer-tray.tsx` (mounted by the shell, so uploads survive navigation) and `drop-zone.tsx` (drag overlay, depth-counted so it does not flicker across rows).
- `uploadFileWithProgress` uses `XMLHttpRequest`: `fetch` exposes no upload progress.

**Not done, and why:** "Purges in N days" needs a retention policy and a purge worker; "Deleted by" needs a `deleted_by` column. Both are migrations, deliberately out of this slice.

## M7 — Search & Command Palette ✅ done

- Migration `0007_search.sql`: `pg_trgm` plus `nodes_name_trgm` (GIN, partial on `deleted_at IS NULL`) and `space_members_by_user`. **This is the first migration since M6 — a deploy needs it before search returns anything.**
- `GET /api/v1/search?q=&space=&kind=&limit=` → node DTO plus `space_name` and `parent_name`, because results span spaces and a bare file name does not identify a hit.
  - Scope is the caller's space membership, joined in the query rather than filtered afterwards, so a row the user may not see is never fetched. **Direct shares deliberately do not widen search** — a shared node is reachable through "Shared with me", and widening here would mean an ancestor walk per candidate row.
  - `q` shorter than 2 runes answers `200 []`, not an error: the palette calls this on every keystroke. `limit` defaults to 20 and clamps at 50.
  - Ranking promotes a prefix match over a mid-string one, then falls back to trigram `similarity()`.
- `parseSearchQuery` holds all query-string validation so it can be unit-tested without a database; `search_test.go` covers the rune-counted minimum, kind validation, limit clamping and the empty-array contract.
- Web: `command-palette.tsx` (⌘K, mounted by the shell), `use-debounced.ts`, `queries/search.ts`. Arrow keys and Enter navigate; a Files / Folders / All chip row maps to `kind`; spaces are matched client-side from the cached list rather than costing a round trip for three rows.
- **The rail field and ⌘K are now different tools.** The rail input filters the open folder locally and instantly; ⌘K searches every space on the server. The `⌘K` chip in the rail is a real button through to the palette, and the field's placeholder reads "Filter this folder" so the distinction is visible rather than implied.

**Not done, and why:** the design's Owner and Modified filter chips need a user picker and a date range, and pagination past 50 needs a cursor — all additive to the same endpoint.

## M8 — Public Share Browsing, Preview Tabs, Editor Chrome ✅ done

- **Anonymous share browsing.** `GET /s/{token}/children`, `/content`, `/node-download`, `/download-all`. Every node id an anonymous caller supplies is walked back up to the share root before anything is served — without that check, one valid token would read any node by guessing ids. A node outside the share answers **404, not 403**: confirming it exists is itself more than the visitor should learn. Owner names are stripped from the response, so the page cannot leak who works at the organisation.
- **Folder download as zip**, for share links and for `GET /api/v1/nodes/{id}/download` on a folder. Streamed entry by entry via `fs.WalkFiles` — nothing is buffered, so a 4 GB folder costs one file handle at a time. An error partway through cannot become a status code, so a failed entry is skipped and the archive completes without it rather than delivering a truncated file that looks whole.
- **Bug:** `handleShareDownload` built `Content-Disposition` as `"attachment; filename=\"" + strconv.Quote(name) + "\""`. `strconv.Quote` adds its own quotes, so browsers saved files with the quotes in the name. Now `mime.FormatMediaType`, which also handles non-ASCII.
- `GET /api/v1/nodes/{id}` added — the full-page editor is routed by node id alone and had no way to learn the file's own name. The breadcrumb trail in `drive-header.tsx` works around the same absence and can now be retired.
- Preview modal restructured: Details / Versions tabs in a right panel, plus a pager across the folder's files (← →). `version-list.tsx` is shared with the ⋯ menu's dialog so the two cannot drift.
- Office editor gains chrome: breadcrumb-aware back, file name, version, and the pp-hub actions OnlyOffice cannot know about (share, versions, download). The ribbon, save indicator and presence stay OnlyOffice's — deliberately not duplicated.

## M9 — Stars, Activity, Recent & Home ✅ done

- Migration `0008_stars_activity.sql`: `node_stars` (composite PK, so starring is idempotent) and `activity_events`.
  - A star is per-user, not a property of the node: two people can disagree about whether a file matters.
  - `activity_events.node_id` is `ON DELETE SET NULL` and the row keeps its own `node_name`, so purging a file does not erase the record that it existed and was deleted.
- `activity.Service` records uploaded / created / renamed / moved / trashed / restored / purged / shared / unshared / reverted. **Recording is best-effort by design** — an activity row describes work that already succeeded, and failing a user's upload because the audit line could not be written would trade a real outcome for a cosmetic one.
- `GET /api/v1/activity` (mine or one space), `/recent`, `/starred`, `/usage`; `POST|DELETE /nodes/{id}/star`. A star needs only _read_ access: it is the user's private note about the node, not a change to it.
- **`used_bytes` is now maintained.** `fs.RecountUsage` runs after every size-changing mutation, so the rail's storage meter reads true instead of always 0. Also best-effort, and logged when it fails.
- Web: cross-space `/drive/home`, `/drive/recent`, `/drive/starred`; `cross-space-table.tsx`, `activity-feed.tsx`, `storage-breakdown.tsx`; star in the node menus; Home / Recent / Starred in Quick Access. **Superseded by M16**, which scoped all three to one space and moved them under `/drive/$spaceId/`.

## M10 — Space Settings & Admin Console ✅ done

- Migration `0009_space_settings.sql`: `spaces` gains slug / description / default_role / archived_at; `space_invitations`; `users.suspended_at`.
- **Invitations exist because `space_members.user_id` references a user row.** An invited address may have no account, so the promise of membership cannot live in that table. Only the token hash is stored; the plaintext is shown once, like a share link. Re-inviting the same address **replaces** the pending row (partial unique index on `(space_id, lower(email)) WHERE accepted_at IS NULL`), so only the newest link works.
- Inviting an address that already has an account adds them as a member directly — an invitation would be a pointless extra step for someone who can already sign in.
- Accepting checks that the invitation's address matches the signed-in account: it is addressed to an email, not to whoever holds the link.
- `PATCH|DELETE /api/v1/spaces/{id}`, `GET|POST /spaces/{id}/invitations`, `DELETE /spaces/{id}/invitations/{inviteID}`, `POST /invitations/accept`, `GET /admin/stats`, `PATCH /admin/users/{userID}`.
- **Suspension is enforced on every request**, not at sign-in, so it takes effect on the next request rather than waiting for an existing session to expire. 403 and not 401: the credentials are fine, the account is not. An admin cannot revoke their own admin flag or suspend themselves — that would leave the server potentially unadministrable with no way back through the UI. (The check moved from the per-handler guard into the `/api/v1` middleware in M20; the rule did not change.)
- Web: `?panel=settings` with General / Members / Danger zone tabs, `/invite/$token`, and a rewritten admin console with stat tiles, suspend/reinstate and admin toggles. **The panel param and the Members tab were replaced in M16** by `settings/general|people|danger` routes.

**Fixed after the fact:** `spaceDTO` never carried `slug` / `description` /
`default_role`, so the settings form read them back as `undefined`, rendered
them empty, and — because it PATCHed the whole set — silently cleared them on
the next save. The DTO now emits them without `omitempty`, the form diffs and
sends only changed fields, and `spaceToDTO` no longer panics on a zero UUID.
Two modals (`preview-modal`, `move-dialog`) held `useState` across reopenings
because better-modal keeps closed modals mounted; both now key their state to
their subject via `useKeyedState`.

**Not done, and why:** the design's Sharing, Storage and Integrations settings tabs have no server behind them, so they are omitted rather than shipped as empty panes. There is no mail transport, so an invitation link is copied to the inviter's clipboard for them to send. Comments (preview modal) and collaborator presence (editor chrome) have no API.

## M11 — Reclaim, Quota, Retention & the Long Tail ✅ done

Migrations `0010_trash_retention.sql`.

**Correctness infrastructure — the part that mattered most:**

- **Blob GC.** `ListUnreferencedBlobs` had existed since M2 with no caller, so every permanent delete leaked its bytes on disk forever. `internal/gc` now sweeps every 15 minutes. Two orderings are deliberate: the sweep only considers blobs unreferenced for **over an hour**, because a blob is written and its ref_count incremented in separate statements and a brand-new blob is legitimately at zero for a moment; and the row is deleted **before** the file, so a crash between them leaves an orphaned file (a leak, findable later) rather than a row promising bytes that are gone (which reads as corruption).
- **Quota is enforced.** `fs.checkQuota` runs at upload, answering **413** when a space would go over. It checks _after_ the bytes land, because a streamed multipart part does not declare its length and the only honest figure is the one measured on the way in; a rejected upload leaves an unreferenced blob that the GC reclaims within the hour. For a new version of an existing file only the size _difference_ counts.
- **Editors can now edit.** `requireNodeAccess` mapped space roles positionally (viewer=1, editor=2, owner=3) against a permission scale where mutation needs 3 — so editors could upload but not rename, move or delete what they had just uploaded. The mapping is now onto the permission scale itself: viewer→view, **editor and owner→edit**. Owner-only powers were never on this path; they are gated by `RequireRole(RoleOwner)` at the space level. Restoring from trash follows delete down to editor — whoever can fill the bin must be able to empty their mistake out of it — while purge and empty-trash stay owner-only because they are irreversible.
- **Trash retention.** `nodes.deleted_by` (nullable; there is no honest way to attribute deletes that predate the column) and `spaces.trash_retention_days`. Retention **defaults to 0, which disables automatic purging** — no existing space has its trash emptied by this being deployed. `gc.TrashWorker` sweeps hourly and is inert until a space opts in. The trash table now shows who deleted an item and a "purges in" countdown that reddens inside three days.

**Remaining design features:**

- **Copy** (`POST /nodes/{id}/copy`). Files copy by _blob reference_: the new version points at the same content and bumps `ref_count`, so duplicating a 2 GB video costs one row. Only the current version carries over — history belongs to the file it happened to. Copying a folder into its own subtree is rejected as a cycle.
- **Search filters**: `owner` and `modified` (a day count, or an RFC 3339 instant). Modified time considers the current version's timestamp for files and the node's own for folders.
- **Member detail**: files owned and last seen, from one query with a correlated count rather than N per-member lookups.
- **Create-space form** now takes slug, description, default role and quota. Applied as a second write after the space exists, so a slug collision rejects the settings rather than losing the space; validation is shared with the update path via `spaceSettingsFrom`.
- **Continue where you left off** on Home, and **keyboard shortcut hints** in both node menus.

**Web component testing.** `@testing-library/react` + `happy-dom`, via a per-file `@vitest-environment` pragma so the other suites stay in node. Testing Library's auto-cleanup only fires when the framework exposes globals, which this suite does not, so `afterEach(cleanup)` is explicit. `use-keyed-state.test.tsx` covers the reopened-modal regression — verified by reverting the hook to a plain `useState`, which fails three of its tests.

**Still not done:** comments (a whole subsystem — table, endpoints, two UIs), mail delivery for invitations (needs SMTP configuration), real PDF rasterisation and video thumbnails (need pdfium/ffmpeg in the image), collaborator presence and save status in the editor (need an OnlyOffice callback), resumable chunked upload (the `uploads` table is still unused, so the tray's pause stops the queue rather than a transfer in flight), and the Sharing/Storage/Integrations settings tabs (no defined requirements).

## M15 — Owner-Anchored Permissions ✅ done

Schema consolidated into a single baseline, `0001_init.sql`, replacing fifteen
incremental files. Acceptable exactly once, pre-production, and stated as such:
from here new changes are new numbered files on top of it.

**The model shrank to two mechanisms.** Access is ownership or a grant, and
nothing else. Ownership is `nodes.created_by`, inherited down the subtree and
resolved in the same recursive query as the grants — so it is not a row anyone
can revoke, and a user cannot lock themselves out of their own work. Everything
a person creates is private to them until they share it.

**Admin stopped meaning access.** The role scale is gone: no
`space_members.role`, no `spaces.default_role`, no `member_role` enum, no
`roleRank`, no `RequireRole`. Admin is membership in the space's automatic
`Admin` group, it gates space administration through `spaces.RequireAdmin`, and
it gates **no file at all**. An admin who opens a colleague's folder sees
nothing, because nothing granted it to them. This was the point of the
milestone: the previous model gave every admin an edit floor over the entire
company tree, which is not a permission model, it is a disclaimer.

**Grants became level + flags.** `shares.perm` (a three-step rank) became
`level` (`view` | `edit`) plus independent `can_download` and `can_share`
booleans, because a rank cannot express "editor who may not re-share" or
"viewer who may pass it on" — both of which people actually ask for. Two DB
CHECKs keep it honest: an editor can always download what it edits, and a link
is always view-only and never re-shareable, so link chains cannot form.
`Caps.Covers` is enforced on create and upsert, so no one hands out more than
they hold (**403**, `share.ErrEscalation`).

**Departure has an answer.** Every space gets an `Archive` folder
(`spaces.archive_root_id`) with one server-made grant: edit+download+share to
the `Admin` group — the single place an admin reads content they do not own.
`RemoveMember` revokes the leaver's grants in both directions, climbs nested
`created_by` to the nearest ancestor owner so work stays with the folder it was
done in, moves their remaining roots under the Archive, then drops their group
rows. Losing the last admin is refused (**409**). Nothing is deleted, and no
live folder silently changes hands.

**Personal folders retired.** `personal_folders`, `spaces.personal_root_id` and
the pinned "My files" carve-out are gone. Ownership does that job for every
node instead of one mapped folder per member, and a member's top level is now
`ListEntryPoints` — the topmost nodes they own or were granted. This also fixed
the bug the old model created: a member could not create a folder at their own
top level, because the space root was admin-only.

Node DTOs now carry `caps`, so the web UI enables the actions the caller
actually holds rather than inferring them from a role it no longer has.

**Tested where it lives.** The invariants are SQL, so
`internal/spaces/permissions_db_test.go` asserts them against a real Postgres
(`PP_TEST_DATABASE_URL`, skipped when unset): an admin resolves to nothing on a
member's folder, ownership inherits down a subtree, promote/demote both take
effect with a last-admin floor, and a departure archives roots while revoking
grants.

## M16 — Login → Account → Space, One Shape ✅ done

No migration. A routing and surface-ownership milestone, fixing three defects
that shared one cause: surfaces that belong to a space did not live under the
space in the URL, and concerns that belong to one owner had two.

**The space became a URL segment.** `/drive/home`, `/drive/recent` and
`/drive/starred` were **siblings** of `/drive/$spaceId`, so on all three
`useParams().spaceId` was undefined and the rail emptied itself: the switcher
read "Select a space", the folder tree vanished, and the Trash and Shared rows
disappeared entirely because they were `?panel=` values needing a space to hang
on. They are now child routes of `drive/$spaceId/`, along with `shared`,
`trash` and `settings/*`. `type DrivePanel`, `onTogglePanel` and every `?panel=`
value are gone. Nothing was papered over with a fallback: there is no state to
lose, only a segment that is always present.

**Membership has one home.** `?panel=members` and the Members tab inside
`?panel=settings` rendered the _same_ component at two addresses, while the
Groups tab edited group membership and declared the automatic `Admin` group
read-only "managed in the Members tab" — so who administers a space was set in
one place and what they could open in another. `settings/people` is now the only
membership surface: one table with an Admin switch, group chips and pending
invitations as rows, beside a group drawer keyed by `?group=`. Admin _is_
membership of the `Admin` group, so all three membership mutations invalidate
`["spaces", id]` — narrowing that to the member list left a promoted admin with
no Admin chip until a reload, which this milestone found and fixed.

**Settings tabs are routes, not state.** Both space settings and the new
`/drive/account` render `settings-tabs.tsx`, a strip of links. Non-admins are
refused in `beforeLoad` rather than shown "only an admin can…" at a URL that
already resolved. `/profile` — a top-level route that dropped the rail, the
breadcrumb and any way back — is replaced by `/drive/account/{profile,preferences}`
inside the shell.

**Feeds are per-space, in SQL.** `GET /api/v1/recent`, `/starred` and `/shared`
take the optional `?space=` that `/activity` and `/usage` already had, via
`sqlc.narg('space_id')`. The filter had to go in SQL rather than the handler:
all three pages are limited, so a post-filter hands back a short page whenever
another space's rows fill the limit first.
`internal/httpapi/space_scope_db_test.go` pins exactly that — with limit 1 the
scoped call still returns the scoped space's row.

**Deep links survive sign-in.** `requireAuth` now redirects to
`/login?redirect=<path>`, and `/auth/login?next=` carries the destination
through the OIDC round trip in a short-lived `pp_oidc_next` cookie, revalidated
on the way back — **folded into the single `pp_oidc_tx` cookie in M20**.
`safeNext` accepts only a same-origin absolute path, so `//evil.example` and
absolute URLs cannot turn sign-in into an open redirect.

**Deliberately not built:** an account Sessions tab (no session-listing
endpoint — **built in M20**, once sessions became rows) and a "leave space"
button (`DELETE /spaces/{id}/members/{userID}` is
`RequireAdmin`, so a member cannot remove themselves — the account page says so
rather than offering a button that 403s). Cross-space Home was **removed**, not
deferred: with every listing scoped to one space, a row from another space could
not be opened without silently switching the rail underneath the user.

**Fixed after the fact:** `ListEntryPoints`' ancestor walk never terminated.
Its recursive step read `SELECT w.origin, p.id … JOIN nodes p ON p.id = w.id`
— re-emitting the node it had just joined, under `UNION ALL`, forever. It
looked harmless because the base step starts at `parent_id`, so a space whose
reachable nodes all sat at the root produced a `NULL` join and stopped on the
first iteration; the moment anyone created a folder _inside_ another, the root
listing (`/children`) and the folder tree (`/tree`) spun until the browser
gave up — 500 after ~10s, with `context canceled` in the server log and the
query still burning CPU in `pg_stat_activity` with no wait event. The step now
climbs to `p.parent_id` and the CTE uses `UNION`, so a `parent_id` cycle can
only ever produce finitely many pairs. The `w.depth > 0` filter went with it:
every walk row is already a strict ancestor, so the filter was silently
excluding immediate parents and only appeared to work because the runaway
recursion re-emitted each parent at every depth.
`TestNestedOwnedNodesAreNotSeparateEntryPoints` pins it with its own 10s
deadline — reverting the query fails it in 10.1s with
`context deadline exceeded` rather than hanging the suite.

## M20 — Sessions Are Rows ✅ done

Migration `0002_sessions.sql` — the first file on top of the frozen baseline,
and the one schema change [production-readiness.md](production-readiness.md)
said Phase A might need.

**A session used to be a signature.** The cookie was
`v1.<uuid>.<expiry>.<hmac>`: self-contained, hard-coded to thirty days, and
backed by nothing, so there was nothing to revoke. A stolen or lost cookie was
good for a month, `HandleLogout` cleared the browser's copy and left the IdP
session standing, and the only lever that invalidated anything — rotating
`PP_SESSION_KEY` — signed out every user at once and rotated the WOPI signing
key with them. It is now an opaque 256-bit random token whose SHA-256 is the
`sessions` row; only the hash is stored, so the table cannot be turned back
into a cookie. `GetSessionByHash` resolves cookie → session + owner in one
indexed join, replacing verify-then-fetch-user. The idle deadline is 30 days,
slid by `TouchSession` at most once an hour per session, under a 90-day
absolute cap measured from `created_at`; `PurgeExpiredSessions` sweeps on a
five-minute ticker in `cmd/server`, which is housekeeping rather than
enforcement — `GetSessionByHash` filters on `expires_at` itself, so an expired
row can never be touched back to life.

**`users.last_seen` finally means last seen.** `TouchSession` updates it in
the same statement that slides the deadline. It used to be written only at
login, so the People page's "last active" column was a login timestamp under
a label that promised more.

**Revocation became a surface.** `GET /api/v1/users/me/sessions`,
`DELETE /api/v1/users/me/sessions/{id}` and `DELETE /api/v1/users/me/sessions`
(all but the current one) back a third tab on `/drive/account`, beside Profile
and Preferences — the tab M16 recorded as deliberately not built because no
session-listing endpoint existed. The user id is part of every delete
predicate, so guessing somebody else's session id achieves nothing; revoking
the session you are holding also clears the cookie, because a browser still
presenting a handle to a deleted row is the same bug seen from the other side.
`POST /auth/logout` deletes the row and returns `{"end_session_url": …}`, and
`signOut()` on the web side hands the browser to it, so sign-out ends the IdP
session too rather than offering a one-click SSO re-entry as the previous
person on a shared machine. That handoff has an ordering the comment in
`lib/queries/auth.ts` records: the global 401 handler is disabled before the
cross-origin assignment, because a mounted query refetching into the session
that was just revoked would otherwise win the race and land on `/login` with
the IdP session still standing. When the provider advertises no end-session
endpoint there is nothing to navigate to, so that branch clears the TanStack
Query cache and routes to `/login` instead. `EndSessionURL` had been there all
along with no caller.

**Authentication is a middleware, not a habit.** `r.Use(s.authenticated)` on
the `/api/v1` group replaces roughly thirty hand-written guards in front of
seventy-odd handlers, with nothing but eyesight connecting the two numbers —
a new route was public until someone noticed. The per-handler guard is gone;
handlers call `caller(r)`, which reads the session the middleware put in the
context and cannot be nil. Called outside the group it panics, because that is a
wiring bug rather than a request anyone can fix. Suspension is still checked
per request, not at sign-in, so suspending an account ends its live sessions
immediately — 403, not 401: the credentials are fine, the account is not.

**The handshake is single-use and constant-time.** `pp_oidc_state` and
`pp_oidc_next` are replaced by one `pp_oidc_tx` cookie carrying state, nonce,
PKCE (S256) verifier and the post-login destination; `HandleCallback` clears
it unconditionally before anything can return early. The pair it replaces
survived every failure path for their full ten minutes, which is a replay
window in exchange for nothing. Both the state and the ID token's nonce are
compared against the transaction with `subtle.ConstantTimeCompare`, and
`next` is revalidated through `safeNext` on the way out even though the cookie
is ours and HttpOnly. A failed callback now redirects to `/login?error=<code>`
— the callback is a top-level navigation, so the problem+json the rest of the
server returns would render as raw JSON in front of somebody who only tried to
log in. The cause stays in the log; `ERRORS` in `apps/web/src/routes/login.tsx`
turns the code into a sentence.

**Cookies carry `Secure`** when `PP_EXTERNAL_URL` is `https://`, beside
HttpOnly and SameSite=Lax, through the one `cookie()` constructor every cookie
in the package goes through. A non-https deployment logs a warning at startup
instead of being quietly interceptable on the first accidental downgrade.

**Split-horizon OIDC stopped being fiction.** `PP_OIDC_ISSUER` is the
discovery URL _this process_ can reach; `PP_OIDC_ISSUER_EXTERNAL` is what the
browser is sent to and what must appear in `iss`. Only the two endpoints the
browser visits — authorization and end-session — are rebased onto the external
origin; token, userinfo and JWKS stay internal because only the server calls
them. Before this the first variable was merely a default for the second and
its doc comment described behaviour that did not exist.

**Changing the issuer no longer locks everyone out.** Identity is keyed on
`(oidc_iss, oidc_sub)`, and `oidc_iss` is a URL an operator can change — a new
IdP hostname, or setting `PP_OIDC_ISSUER_EXTERNAL` for the first time. Every
returning user then looked brand new, the insert collided with
`users_tenant_email_hash`, and sign-in answered 500 for everybody with no way
back short of hand-editing Postgres. A miss on the identity now falls back to
the verified email and re-points the account (`AdoptUserIdentity`), gated on
`email_verified` because an IdP that lets a user claim an arbitrary address
would otherwise let them claim an arbitrary account. A real collision answers
`email-taken` on the sign-in page instead of a 500.

**One key, purpose-bound subkeys.** `config.SessionKey` is now `SecretKey`, and
nothing signs with it directly: `DeriveKey` (HKDF-SHA256) issues a subkey per
purpose and WOPI tokens take `wopi.KeyPurpose`. The environment variable is
still `PP_SESSION_KEY` on purpose — renaming it would silently fall back to an
ephemeral key in every existing deployment and break office previews at
runtime instead of at startup. Sessions no longer touch it, so rotating it
signs nobody out; it invalidates in-flight WOPI tokens only.

**Frontend:** the thin second identity endpoint is deleted —
`GET /api/v1/users/me` is the one identity endpoint, cached under `keys.me`,
so the rail and the profile page can no longer hold two versions of the same
person. A global 401 handler in `main.tsx` clears the query cache and
navigates to `/login` (excluding `/login` itself, whose own probe legitimately
401s), because a session can now be ended from another device while a tab is
still rendering a signed-in shell.

**Testing.** `internal/auth/sessions_db_test.go` follows the established
`PP_TEST_DATABASE_URL`-or-skip contract — the lookup join, expiry refusal,
`TouchSession` moving both the deadline and `last_seen`, owner-scoped
revocation, "sign out everywhere else" keeping the current row, and the list
excluding expired sessions are all invariants that live in SQL and cannot be
asserted against a fake. `TestEveryAPIRouteIsAuthenticated`
(`internal/httpapi/routes_test.go`) is the structural guard: it walks the
router and fails if any `/api/v1` route is not wrapped by `authenticated`,
which is what makes "the group is the guard" enforceable rather than a
convention.

**Deferred here, shipped in M21:** an `Origin`/`Referer` CSRF check. The
reasoning at the time was that `SameSite=Lax` plus a JSON-over-`fetch` API is
thin rather than absent, which was true and was also the argument for a single
control being the only one. See M21.

## M21 — Access Re-Resolved, and the Controls That Were Missing ✅ done

Migration `0003_security_events.sql`. A security milestone rather than a
product one: no new surface except an admin-readable log, and one route group
(`/wopi/`) removed from nginx.

**The through-line is M20's.** Making a session a row was worth doing because
it turned "decide once, trust forever" into "re-resolve on every request".
Everything else in the system that still decided access once was then worth
finding, and there were three. A public share link resolved its token hash and
expiry and nothing else, so revoking the grant that let somebody share a
folder left every link they had minted alive, anonymously, indefinitely —
`resolveLinkNode` now re-resolves `EffectiveCapsForNode` for the share's
`created_by` and answers 404 once that no longer holds `CapShare`. A WOPI
access token carried a permission rank decided when the editor opened, and
`GetFile` accepted rank 1 — so an editor session was the way around the
no-download half of a grant: mint a view token, let the Document Server fetch
the document, keep the file, while `handleContent` refused the same person for
the same bytes. `GetFile` now requires rank ≥ 2 for a user token and
`handleEditSession` requires `CapDownload`, minting rank 2 even for a
read-only session because rank 1 could no longer fetch its own document; a
machine token (`wopi.SystemSubject`) is unaffected, having no subject to
resolve a capability for. And the caller's own IP address was the caller's
choice, which made every future limit keyed on it decorative.

**The view/download split is now a rule rather than a label.**
`GET /s/{token}/content` ignored the link's `can_download` and served
byte-identical originals — of every file in the subtree, one `?node=` at a
time. `GET /nodes/{id}/preview?kind=text` is a verbatim 4 KiB prefix of the
source, which for a note, a CSV or a key is the whole document, and it was
gated on view. Both take download now; `kind=thumbnail` stays on view,
because a 256px rasterization is genuinely derived and it is what lets a
no-download reader see _something_. The accepted cost is stated rather than
hidden: a reader with view-but-not-download cannot open an office document at
all, only its thumbnail, and an anonymous no-download link is honestly names,
sizes and dates — there is no anonymous preview endpoint. See
[permissions.md](permissions.md).

**Two controls did not exist at all.** `Server.sameOrigin` wraps the
`/api/v1` group and `/auth/logout`, compares `Origin` — or the _origin_ of
`Referer`, never its path — against `PP_EXTERNAL_URL`, and fails closed if
that value will not parse. It runs before `authenticated` on purpose: a forged
cross-site write should cost no session lookup and should be told the real
reason rather than a 401. `/wopi/*` is exempt because the Document Server
sends no Origin at all and is authorized by a signed token, and
`/auth/callback` is exempt because it is a top-level navigation from the IdP
defended by the state and nonce in `pp_oidc_tx`. Alongside it, per-IP token
buckets: `/auth/login`, `/auth/callback` and `/auth/logout` at 30/min burst
10, `/s/{token}` at 60/min burst 20 — a share token is a guessable secret
whose hit answers 200 — and `/api/v1` at 1200/min burst 200. They are
in-process, so N replicas allow N× the rate; that is written down in
[deployment.md](../deployment.md) rather than left for an operator to
discover. Upload bodies gained a ceiling too: `PP_MAX_UPLOAD_BYTES` (5G
default) through `http.MaxBytesReader`, because `ParseMultipartForm`'s
argument was never a limit — it is the in-memory spill threshold — and quota
is checked only after the bytes land, from a default of unlimited.

**The audit trail is a second table, not more verbs on the first.**
`activity_events` cannot hold an authentication event: `space_id` and
`actor_id` are both `NOT NULL`, an auth event belongs to no space, and the
interesting one — a failed sign-in — may have no account to name.
`security_events` plus `internal/audit` record `login`, `login_failed`,
`logout`, `session_revoked`, `sessions_revoked`, `identity_adopted`,
`admin_granted`, `admin_revoked`, `account_suspended`,
`account_unsuspended`, `grant_created`, `grant_revoked`, `share_revoked`,
`member_added` and `member_removed`, each with the client IP, a truncated user
agent and chi's request id, so a row joins to the access-log line that
produced it. `GET /api/v1/admin/security-events` pages it newest first,
admin-only and deliberately instance-wide, since the value of a run of failed
sign-ins is seeing it across accounts and addresses at once. Retention is
`PP_AUDIT_RETENTION_DAYS` (365), purged by the same five-minute ticker that
sweeps expired sessions and WOPI locks. Read auditing is still absent, and
deliberately: a `downloaded` row per byte-serving request is the
highest-volume event in the system and needs sampling and a retention policy
of its own. See [data-model.md](data-model.md).

**The first admin is a configuration value.** `PP_BOOTSTRAP_ADMIN_EMAIL`
promotes whoever signs in with that address, on every sign-in — idempotent,
and therefore also the recovery path for an install whose last admin was
demoted. It requires a **verified** email claim, because the address is
matched against a static configuration value and would otherwise be a standing
authorization rule keyed on a field a self-service IdP may let its users edit.
The same asymmetry existed one layer down: `TouchUserLogin` used to rewrite
`users.email` from an unverified claim on every sign-in, and that column is
what invitation acceptance matches on, what a direct share is granted to, and
what this variable is compared against. It no longer does.

**Provenance.** `middleware.ClientIPFromXFF(PP_TRUSTED_PROXIES...)` replaces
chi's deprecated `RealIP`, which believed `True-Client-IP`, `X-Real-IP` and
`X-Forwarded-For` from anybody; it is installed only when the variable names
at least one CIDR, and everything reads the result through `audit.ClientIP`,
where "nothing vouched for" means "believe the TCP peer". `config.Load`
validates the CIDRs so a typo is a sentence rather than a panic out of chi,
and nginx clears `True-Client-IP`, `CF-Connecting-IP` and `Forwarded`.
Credentials also stopped reaching the logs: `redactingLogger` rewrites
`r.RequestURI` — the raw string chi's Logger formats — so `/s/<token>`
becomes `/s/REDACTED` and `access_token=` becomes `REDACTED`, while `r.URL`
is left untouched because chi routes off `r.URL.Path` and the WOPI handler
needs the credential it was handed. An earlier attempt swapped the URL as
well and every share link answered 404 with a log line that looked perfect.
nginx does the same job with a `map` and a custom `log_format`.

**Edge and deployment.** `nginx.conf` became an envsubst template (the CSP has
to name the Document Server's origin) and now sends CSP, HSTS,
`X-Frame-Options: DENY`, `Referrer-Policy` and `nosniff`, with
`server_tokens off`; the headers are repeated inside `location /assets/` and
`location = /index.html` because nginx does not merge `add_header` across
levels. Two CSP directives are deliberately loose — `object-src 'self'` for
the PDF viewer's `<object>`, and `form-action`/`frame-src` naming
`${PP_ONLYOFFICE_URL}` for the editor's cross-origin form POST — and
`style-src 'unsafe-inline'` is the one that needs code changes to tighten.
`/wopi/` is no longer proxied at all: no shipped topology used it, and it was
the one route group with neither an origin check nor a rate limit. Compose
splits one flat network into `edge`/`data`/`office`, so OnlyOffice can no
longer open a connection to Postgres or SeaweedFS; `web` gains
`no-new-privileges`, `cap_drop: ALL` plus `NET_BIND_SERVICE`, and an nginx
master running as `nginx`. `apps/server/.dockerignore` now exists, because
Docker reads that file from the root of the build context and these images
build with `context: ../../apps/server` — so a bare `COPY . .` had been
pulling `.env` and `data/blobs` into builder layers that a `--cache-to`
registry export would publish.

**Also closed, smaller:** `DeleteShare` is scoped by node, so a caller who can
share one node can no longer revoke a share on another; `spaces.SetAdmin`
refuses a non-member, which used to be able to seat an admin the People page
does not list; `application/octet-stream` left `isOfficeMime`, where it had
made the allowlist mean "any file with an unfamiliar extension"; and the two
byte-serving endpoints that sent no `nosniff` and no `Content-Disposition` —
WOPI `GetFile` and the anonymous share-file path — now send both.

**Testing.** The guards are structural where they can be.
`TestEveryAPIMutationChecksOrigin` walks the router and fails if any non-safe
`/api/v1` method is not wrapped by `sameOrigin`;
`TestWopiIsExemptFromOriginChecks` fails if the WOPI group ever _is_, because
that would break every office save — the two together are the same
"convention nobody can forget" that `TestEveryAPIRouteIsAuthenticated`
established in M20. `csrf_test.go` covers the middleware itself: own origin
accepted, a host that merely starts with the real one refused
(`drive.example.com.evil.example`), a scheme downgrade and a different port
refused, the `Referer` fallback compared by origin only, both headers absent
refused, every unsafe method covered, safe methods untouched, and an
unparseable `PP_EXTERNAL_URL` refusing everything rather than allowing it.
`ratelimit_test.go` holds the bucket arithmetic — burst spent then refused,
refill over time, keys isolated from each other, and idle buckets evicted so
an attacker-controlled key is not a memory leak. `wopi_db_test.go` gained
`TestWopiRefusesViewOnlyReads` (rank 1 cannot `GetFile`) and
`TestWopiMachineTokenIsAuthorizedWithoutACapability` (the worker's fetch still
works) beside the M20 cases for revocation, departure, suspension, downgrade
and token/file binding. `logging_test.go` covers both halves of the
redaction: that a share token and a WOPI `access_token` are replaced in the
logged URI and ordinary paths are left alone, and — the regression a unit test
on the helper alone would have missed — that routing and the handler still see
the real values, since chi matches on `r.URL.Path` and the WOPI handler has to
read the credential it was given.

## Testing Strategy

- **Unit**: capability resolver matrix (ownership × grant × ancestor × link), blob CAS (put/get/dedup/gc), tree ops (move-cycle rejection, rename conflicts).
- **Database-backed**: the invariants that live in SQL — owner inheritance, admin-has-no-access, member lifecycle, session lookup and revocation, and WOPI access re-resolved against live grants — run against a real Postgres and skip without `PP_TEST_DATABASE_URL`.
- **Structural**: three router walks. `TestEveryAPIRouteIsAuthenticated` fails if an `/api/v1` route is not wrapped by the auth middleware, `TestEveryAPIMutationChecksOrigin` fails if a non-safe `/api/v1` method is not wrapped by `sameOrigin`, and `TestWopiIsExemptFromOriginChecks` fails if the WOPI group ever is. A convention nobody can forget is worth more than a convention everybody is told about.
- **Integration** (compose test profile): upload→download byte-identical roundtrip; WOPI flow against real OnlyOffice (open → PutFile → version increments); link share access without auth.
- **Contract**: hand-typed TS interfaces in `apps/web/src/lib/api/types.ts` mirror the Go handlers; OpenAPI codegen deferred until the surface grows.
- **Load sanity**: k6 — 100 concurrent uploads, 5-user co-edit; pass = no errors, no performance target yet.

## Rollout / Operations

- Two compose files in `deploy/compose/`: `compose.dev.yml` (services only, app runs from source) and `compose.prod.yml` (everything, for a VPS or Dokploy). Config via `.env`, templated by `.env.example` in each location.
- Persistent state: `data/` volume (blobs) + Postgres volume.
- Backup: `pg_dump` + rsync of `data/blobs` — **documented, not implemented**. See
  [production-readiness.md](production-readiness.md), which collects everything
  standing between M21 and an internal rollout: the P0 blockers that remain
  (backup, mail transport, and lock enforcement on the REST version-upload
  path — the session/cookie, OnlyOffice JWT, admin bootstrap and
  upload/rate-limit items are closed, as are the sweep findings in its §8),
  the P1 gaps against Google Drive (content search, read auditing and an audit
  export, comments, resumable upload, storage accounting, IdP group sync), and
  a proposed phasing.
