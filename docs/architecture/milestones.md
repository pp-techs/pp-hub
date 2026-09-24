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

**Upload queue** — client-side against the existing `POST /spaces/{id}/files`. **Superseded by M30**: that endpoint is gone, and the queue drives resumable tus uploads through `lib/api/tus.ts`.

- `lib/upload-store.ts`: queue with a concurrency limit of 3, per-file progress, cancel, retry, pause; a promise-keyed mkdir cache so a 400-file folder issues one mkdir per directory, not per file.
- `lib/drop-items.ts`: flattens a drop through `webkitGetAsEntry`, draining `readEntries` in a loop (it returns at most 100 per call), and a folder pick through `webkitRelativePath`.
- `transfer-tray.tsx` (mounted by the shell, so uploads survive navigation) and `drop-zone.tsx` (drag overlay, depth-counted so it does not flicker across rows).
- `uploadFileWithProgress` uses `XMLHttpRequest`: `fetch` exposes no upload progress. M30 replaced it with the tus client's PATCH, which uses `XMLHttpRequest` for the same reason.

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
- **Invitations exist because `space_members.user_id` references a user row.** An invited address may have no account, so the promise of membership cannot live in that table. Only the token hash is stored; the plaintext is shown once, like a share link. Re-inviting the same address **replaces** the pending row (partial unique index on `(space_id, lower(email)) WHERE accepted_at IS NULL`), so only the newest link works. **All of the invitation half of this milestone was superseded in M33**, which made membership a projection of a Logto organization: `space_invitations`, the four routes below and `/invite/$token` are gone, and an invitation is now a Logto organization invitation. The prose here is kept as it shipped.
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

**Not done, and why:** the design's Sharing, Storage and Integrations settings tabs have no server behind them, so they are omitted rather than shipped as empty panes. There is no mail transport, so an invitation link is copied to the inviter's clipboard for them to send (an SMTP transport for invitations shipped in **M23**). Comments (preview modal) and collaborator presence (editor chrome) have no API.

## M11 — Reclaim, Quota, Retention & the Long Tail ✅ done

Migrations `0010_trash_retention.sql`.

**Correctness infrastructure — the part that mattered most:**

- **Blob GC.** `ListUnreferencedBlobs` had existed since M2 with no caller, so every permanent delete leaked its bytes on disk forever. `internal/gc` now sweeps every 15 minutes. Two orderings are deliberate: the sweep only considers blobs unreferenced for **over an hour**, because a blob is written and its ref_count incremented in separate statements and a brand-new blob is legitimately at zero for a moment; and the row is deleted **before** the file, so a crash between them leaves an orphaned file (a leak, findable later) rather than a row promising bytes that are gone (which reads as corruption).
- **Quota is enforced.** `fs.checkQuota` runs at upload, answering **413** when a space would go over. It checks _after_ the bytes land, because a streamed multipart part does not declare its length and the only honest figure is the one measured on the way in; a rejected upload leaves an unreferenced blob that the GC reclaims within the hour. For a new version of an existing file only the size _difference_ counts. **Superseded by M30**: `fs.EnsureRoom` counts every stored version, trash included, so a new version costs its full size, and a tus upload is also refused at creation against its declared length.
- **Editors can now edit.** `requireNodeAccess` mapped space roles positionally (viewer=1, editor=2, owner=3) against a permission scale where mutation needs 3 — so editors could upload but not rename, move or delete what they had just uploaded. The mapping is now onto the permission scale itself: viewer→view, **editor and owner→edit**. Owner-only powers were never on this path; they are gated by `RequireRole(RoleOwner)` at the space level. Restoring from trash follows delete down to editor — whoever can fill the bin must be able to empty their mistake out of it — while purge and empty-trash stay owner-only because they are irreversible.
- **Trash retention.** `nodes.deleted_by` (nullable; there is no honest way to attribute deletes that predate the column) and `spaces.trash_retention_days`. Retention **defaults to 0, which disables automatic purging** — no existing space has its trash emptied by this being deployed. `gc.TrashWorker` sweeps hourly and is inert until a space opts in. The trash table now shows who deleted an item and a "purges in" countdown that reddens inside three days.

**Remaining design features:**

- **Copy** (`POST /nodes/{id}/copy`). Files copy by _blob reference_: the new version points at the same content and bumps `ref_count`, so duplicating a 2 GB video costs one row. Only the current version carries over — history belongs to the file it happened to. Copying a folder into its own subtree is rejected as a cycle.
- **Search filters**: `owner` and `modified` (a day count, or an RFC 3339 instant). Modified time considers the current version's timestamp for files and the node's own for folders.
- **Member detail**: files owned and last seen, from one query with a correlated count rather than N per-member lookups.
- **Create-space form** now takes slug, description, default role and quota. Applied as a second write after the space exists, so a slug collision rejects the settings rather than losing the space; validation is shared with the update path via `spaceSettingsFrom`.
- **Continue where you left off** on Home, and **keyboard shortcut hints** in both node menus.

**Web component testing.** `@testing-library/react` + `happy-dom`, via a per-file `@vitest-environment` pragma so the other suites stay in node. Testing Library's auto-cleanup only fires when the framework exposes globals, which this suite does not, so `afterEach(cleanup)` is explicit. `use-keyed-state.test.tsx` covers the reopened-modal regression — verified by reverting the hook to a plain `useState`, which fails three of its tests.

**Still not done:** comments (a whole subsystem — table, endpoints, two UIs), mail delivery for invitations (needs SMTP configuration), real PDF rasterisation and video thumbnails (need pdfium/ffmpeg in the image), collaborator presence and save status in the editor (need an OnlyOffice callback), resumable chunked upload (the `uploads` table is still unused, so the tray's pause stops the queue rather than a transfer in flight — **done in M30**), and the Sharing/Storage/Integrations settings tabs (no defined requirements).

## M12–M14 — Numbers That Were Skipped ⏭ never existed

There is no M12, M13 or M14, and nothing was folded into another entry: the
numbering simply skipped. `M12` was used exactly once anywhere in this
repository — as the header comment of `0012_personal_folders.sql`
(`-- M12: Personal folders.`), where those headers named the milestone rather
than the file, which is why `0010_trash_retention.sql` and `0011_groups.sql`
both say `M11`. The label and the migration went away together in M15, which
retired `personal_folders` and `spaces.personal_root_id` in favour of
ownership. M13 and M14 exist nowhere: no entry here, no commit, no migration
header, no document. The work that did land between M11 and M15 was recorded
where it belonged rather than numbered — real PDF and video thumbnails in the
optional `cmd/worker` container are noted as delivered post-M5 in M5 above,
and the S3 blob backend, the deployable images and the two compose files are
in [storage.md](storage.md), [deployment.md](../deployment.md) and Rollout /
Operations below.

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

## M16 — Login → Account → Space, One Shape ✅ done · settings surfaces superseded by M34

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

> **M34 superseded this paragraph.** The invariant it establishes — the router
> owns which pane is open, never component state — still holds; the ten routes
> and two tab strips it created do not. Settings is one hash-addressed dialog
> now.

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

## M17 — One Dialog Owns Access ✅ done

No migration. On the server it is one new query and one narrowed one; the rest
is the surface that sat on top of them.

**Sharing was split across two dialogs that each answered half the question.**
`share-panel.tsx` offered direct-by-email and link shares behind a bare
`<input type="email">`, while group access lived in a folder-only
`folder-access-dialog.tsx` behind "Who can open this…". The split was
client-side invention: `Grant` only checks that the node and the group share a
space, and the resolver treats a share row on a file exactly like one on a
folder, so the server had always allowed a file to be given to a group and
only the UI refused to offer it. The same narrowness showed in the share list,
which rendered a group grant as a row labelled with the literal word "group",
because `Share.kind` was a bare string rather than the closed union the server
emits. Both are now one `share-dialog.tsx` on any node — groups, people and
the public link in one merged access list, one level/download/re-share control
applied to everyone picked — and `?panel=share` is gone with them
(`PanelKind` is now just `versions`).

**Grantees come from the space, never from a tenant-wide directory.** One
`grantee-picker.tsx` combobox offers the space's groups above its members and
filters locally, because `GET /spaces/{id}/groups` and
`GET /spaces/{id}/members` are both member-readable and already cached — no
new endpoint and no round trip per keystroke. This was also a leak: `SearchUsers`
behind `GET /users/search` had no space scoping at all, so any signed-in user
could enumerate every account on the instance, which under M11's "a space is a
company" is a company boundary. It now joins `space_members` against the
caller and matches only accounts sharing a space with them, lifted for an
instance admin, pinned by `SearchIsLimitedToSharedSpaces` against a real
Postgres. The group drawer's member-add picker moved off that search onto
`GET /spaces/{id}/members` — which is what the server already enforced with a
422 for an outsider; the picker simply no longer offers one to click. The
invite modal keeps the tenant-wide search, since inviting somebody who is not
a member yet is its one legitimate use.

**The shared-folder indicator was reading the wrong list.** It took `isShared`
from `useShared(spaceId)` — `GET /shared`, i.e. _shared with me_ — which by
construction excludes every node the caller owns, so a folder you created and
then shared out showed no indicator at all. It is now a server-set
`node.shared`: `attachCaps` batch-resolves `NodesWithLiveShares` (any live
direct, group or link row on the node itself, any grantee) alongside the caps
batch it already ran, so one pass answers both "what may I do here" and "did
anyone share this at all" for every row in a listing. The folder browser's
separate `useShared` call — kept only to feed the old badge — is gone, while
the "Shared with me" page still uses it for its actual list. The indicator is
not an overlay: a shared folder renders `default-folder-opened` in place of
`default-folder`, rather than a glyph stamped on the corner of an icon that
already means something. A team folder renders `folder-type-public`.

**Fixed on the way:** typing into either search input produced no visible
characters. base-ui's `Menu` popup runs a bubble-phase keydown handler for
roving focus and typeahead that calls `preventDefault()` on printable keys, so
an `<Input>` nested inside `DropdownMenuContent` lost every keystroke while
keeping focus throughout — which is why it looked like a render bug rather
than an event one. The input now stops propagation on keydown, Escape
excepted so the menu still closes. The defect was pre-existing in
`member-picker.tsx` and had been copied into the new picker before it was
found.

## M18 — The Shell Is the shadcn Dashboard Shell ✅ done

No migration; `apps/web` and `packages/ui` only.

**A hand-rolled rail cannot do what a rail has to do.** `drive.tsx` is now
`SidebarProvider` → `app-sidebar.tsx` (`variant="inset"`,
`collapsible="offcanvas"`) → `SidebarInset`, so the rail collapses on ⌘/Ctrl+B,
remembers that in a `sidebar_state` cookie and becomes a sheet below `md` —
none of which the `<aside className="w-64">` it replaces could do at all.

**The bigger win is that there is now one header.** `site-header.tsx` renders
the sidebar trigger, a breadcrumb derived from the pathname, the ⌘K palette
button and a portal slot; every surface fills that slot through
`HeaderActions` instead of stacking a second 40px band of its own — Home and
Account each used to, both an `h-10` strip with a border under it. The slot is
a portal rather than a field in `ui-store` deliberately: a `ReactNode` is not
UI state, and keeping it in `header-slot.tsx` means no effect writes on render
and no stale node left behind when a route unmounts. `drive-header.tsx`,
`drive-toolbar.tsx` (dead: defined, never imported), `quick-access.tsx` and
`rail-item.tsx` are gone, the listing's own controls moved to
`listing-controls.tsx`, rail rows are `SidebarMenuButton` /
`SidebarMenuSubButton`, and `RailLabel` — which was never used in the rail —
is `section-label.tsx`.

**Page rhythm has one home.** `surface.tsx` (`@container/main`, `px-4 lg:px-6`,
`py-4 md:py-6`) is the scrolling body of every surface _except_ the folder
listing, which opts out on purpose: a file manager's rows have to reach the
edges and scroll under a sticky header, so it keeps a full-bleed container
clipped to the inset's corner. The container query rather than a breakpoint
matters because the details panel narrows that column without the route
knowing.

**Two invariants are load-bearing, and both are written into the shell rather
than left to be rediscovered.** `SidebarInset` has to stay a _direct sibling_
of the sidebar: its margin, rounding and collapsed-state offset are all
`md:peer-data-[variant=inset]:…`, and a `peer-` selector only matches a
preceding sibling — so `HeaderSlotProvider` and `DriveWriteProvider` (M19)
wrap `SidebarProvider` and never sit between it and the inset. And the shell
has to pin `h-svh overflow-hidden` on the provider, because `SidebarProvider`
is only `min-h-svh`: without it the listing grows the page instead of its own
scroller, which is also what the marquee band measures against.

## M19 — One Owner Decides Where a Write Lands ✅ done

No migration; `apps/web` only.

**The rail's New menu is a sibling of the folder listing, not a descendant of
it, and it used to guess.** It hardcoded the space root, so uploading from
inside a folder silently landed at the top of the space — two components
deriving the same destination independently, and therefore able to disagree.
`DriveWriteProvider` (`components/drive-write.tsx`) is now the sole owner,
mounted in `drive.tsx` above the sidebar and the inset alike, and
`writeTarget` (`lib/write-target.ts`) derives the destination from `?dir=` the
way `folder-tree.tsx` already did. `?dir=` is the only truth both sides share,
and a surface that has none — Home, Recent, Starred, Shared, Trash, Account —
writes to the space root by definition rather than by fallback. The same
function answers whether the write is allowed: any member may create at the
space root and owns what they create there, while inside a folder it takes
edit capability on that folder.

**The provider owns the machinery too**, which is what stops it drifting back:
both hidden file inputs (two, because `webkitdirectory` is a property of the
element rather than of the click, so picking files and picking a folder cannot
share one control) and the New-folder dialog move out of `drive.tsx` and
`$spaceId/index.tsx`. `ui-store`'s `newFolderOpen` flag — the only way that
dialog used to reach across the rail/listing boundary — went with them, and is
now ordinary state inside the provider that owns both ends of it.

**`folderActions()` (`components/folder-actions.tsx`) is `nodeActions()`'s
counterpart for the folder itself**: New folder, Upload files, Upload folder,
Paste, Select all, Sort by, View, Refresh and Folder information, rendered
both by the rail's dropdown and by a new background `ContextMenu` on the
listing. Its trigger is the scroller itself, so it answers on the gaps between
rows, below the last one and over the empty state — everywhere the folder
itself is what was clicked — while rows stop the event at their own trigger so
only one menu ever opens. A background right-click also clears the selection,
because a menu that acts on the folder must not read as acting on whatever was
selected a moment ago.

**Paste needs a source.** Nodes are Cut or Copied onto a small
`ui-store.clipboard`, deliberately outside `partialize` so it never survives a
reload: a clipboard that outlives the page offers to move nodes that may no
longer be there. `pasteTargets` returns the nodes a paste would actually land,
so the disabled state and the executor cannot disagree about which ones move —
a cut is refused into the folder the node already sits in and into the node's
own subtree, which would detach that subtree from the root, while a copy into
the original's folder stays offered because that is the ordinary "make a copy".
The ancestor walk carries a `seen` set, since `dirs` is a client-side trail
and a cycle in it is not impossible. `tests/write-target.test.ts` covers the
arithmetic.

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
is checked only after the bytes land, from a default of unlimited. (Since
M30 browser uploads arrive only through tus, where the ceiling is checked
against the declared `Upload-Length` at creation and each chunk is bounded
by it; `limitBody` still guards WOPI `PutFile` and the avatar upload.)

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
Credentials also stopped reaching the logs: `redactingLogger` rewrote
`r.RequestURI` — the raw string chi's Logger formats — so `/s/<token>`
became `/s/REDACTED` and `access_token=` became `REDACTED`, while `r.URL`
was left untouched because chi routes off `r.URL.Path` and the WOPI handler
needs the credential it was handed. An earlier attempt swapped the URL as
well and every share link answered 404 with a log line that looked perfect.
nginx does the same job with a `map` and a custom `log_format`. (M25 replaced
chi's Logger with `observe`, which logs `redactURI`'s output and still never
rewrites the request.)

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

## M22 — Backup and Restore ✅ done

P0 §1. There was no backup of any kind: no script, no Makefile target, no
volume snapshot, no CI job. `pg_data` and `blob_data` had to be captured
**together and consistently**, because a database dump restored without its
blobs is a directory listing of files that no longer exist and blobs
restored without the database are content-addressed garbage with no names.

**What shipped.** `deploy/backup/pp-backup.sh` and `pp-restore.sh`, two
POSIX `sh` scripts driving `docker compose` against `compose.prod.yml` —
no volume is ever named directly, since the file carries no top-level
`name:`, so every volume is reached through `docker compose run`/`exec` on
the service that mounts it. Postgres is dumped before blobs are archived,
never the reverse, because the one ordering that must never invert is a
blob a fresh Postgres row already references but whose bytes missed the
archive; both the dump and the archive are verified (`pg_restore --list`,
`tar -tzf`) before the script calls itself done, and retention defaults to
14 days. `pp-restore.sh` verifies the archive before stopping anything,
restores with `pg_restore --clean --if-exists --no-owner`, restores blobs,
brings the stack back up and polls `/readyz` for 60s. `.env.prod` — the
Postgres password, `PP_SESSION_KEY`, OIDC secrets — is explicitly out of
scope and the backup says so on every run, because a restore without it
comes up as a different instance.

**A restore has actually been performed**, not merely scripted: against a
built `compose.prod.yml` stack, a drill user row and a marker blob were
written, backed up, destroyed (`DROP SCHEMA public CASCADE`, `rm -rf
/data/blobs/*`), and restored — the row, the blob's bytes, and
`schema_migrations`' newest version all came back identical.

## M23 — Mail Transport, Invitations Only ✅ done · superseded by M33

> The only message `internal/mail` ever sent was the space invitation, and
> **M33 moved invitation email to Logto**. The package and the `PP_SMTP_*`
> variables are deleted; there is no SMTP transport in the product today. The
> entry below is left as it shipped rather than rewritten — it is why P0 §2
> was closed, and M29 still needs a transport, which it will have to choose
> again.

P0 §2. There was no mailer and no in-app substitute for one:
`handleCreateInvitation` minted a token and handed the URL back to the
admin to deliver by hand — one copy-paste into a chat client per person.

**Decision taken: SMTP for invitations only.** Sharing still notifies
nobody, and there is still no in-app substitute — no `notifications` table,
no route, no SSE or websocket. That half is M29's; it needs a notification
story, not a transport, and it reuses the one built here rather than waiting
on it.

**What shipped.** `internal/mail`, a small `net/smtp` wrapper — dial,
optional STARTTLS or implicit TLS, optional PLAIN auth, header-injection
rejected on any `\r`/`\n` in the recipient or subject. `config.validateSMTP`
fails startup rather than a send on a half-configured transport (host set,
no `PP_SMTP_FROM`; an unrecognised `PP_SMTP_TLS`; a port out of range);
`Host == ""` is a supported, logged state. `handleCreateInvitation` sends
synchronously under a 15s timeout after the invitation row is committed; a
send failure is logged and the response still carries the URL, so the
admin's fallback is the product's old and only behaviour rather than a new
failure mode. The response gains `mail_sent`; the web client branches its
toast on it and keeps the clipboard copy in both paths. Verified against a
real relay, not just unit tests: a message sent through a MailHog catcher
arrived.

## M24 — The REST/WOPI Write Collision ✅ done

P0 §7, the only P0 item that lost data during ordinary, well-intentioned
use. `file_locks` exists with a 30-minute TTL and the WOPI endpoints honour
it; nothing else did — no `GetLock` on the REST path, no lock route under
`/api/v1` — so while one person had a document open in OnlyOffice, another's
`PUT /nodes/{id}/versions` was accepted against no lock at all, and the next
save silently discarded one side of the conflict.

**Decision taken: refuse, do not take a lock.** A REST upload has no lock
lifecycle — no unlock call, no `REFRESH_LOCK` — so a lock taken here would
only be released by the sweep, which is worse than the occasional annoyance
of an editor left open over lunch blocking a colleague.

**What shipped.** `Server.lockedByEditor`, called from both
`handleUploadVersion` (before `parseUpload`, so a locked file is refused
before a multi-gigabyte body is read) and `handleRestoreVersion` (restoring
a version repoints `current_version_id` just as directly). Either finds a
live lock, it answers `409 Conflict` with a sentence a client can show.
Deliberately out of scope: trash, move and rename of a locked node — none
change file bytes, and `handleWopiPutFile` already refuses a tombstone.
`TestRestVersionUploadRefusesALockedFile` holds the line against a real
Postgres.

**Superseded in part by M30**: `handleUploadVersion` is gone. A replacement
is now a tus upload naming `node_id`, and `lockedByEditor` — which now takes
the status to answer — runs at creation and again at commit, answering
**423 Locked** there because tus reserves 409 for an offset mismatch;
restore-version still answers 409.
`TestReplacementUploadHonoursTheEditorLock` replaced the REST-upload test.

## M33 — Membership Is a Logto Organization ✅ done

Membership was defined twice. An operator created the person in Logto so they
could sign in, then invited the same address into a space in pp-hub so they
could see anything: two consoles, two mental models, two lists that drifted
apart the first time somebody was off-boarded in one of them.

**Decision taken: a Logto organization is the source of truth for who belongs
to a company.** pp-hub stops being a second place where that is decided and
becomes the place where it is applied. `spaces.idp_org_id` names the
organization a space mirrors, and `space_members` becomes a **projection** of
its member list — a table still, because ten SQL sites join it to scope a
query and thirteen Go guards read it, but one with exactly one writer. The
organization role `admin` maps onto the automatic `Admin` group; user-created
groups, grants, ownership, quota and the Archive are untouched, because a
tenant-wide organization template cannot express a per-space group and should
not try.

**This supersedes the invitation work in M9 and M23** — `space_invitations`,
the four invitation routes, `POST /invitations/accept`, `/invite/$token`,
`internal/mail` and the `PP_SMTP_*` variables are all gone. Those entries
stay as written: they were true when they shipped, and rewriting them would
hide the fact that a local invitation system existed, was used, and was
replaced rather than merely never built. An invitation is now a Logto
organization invitation, delivered by Logto's own email connector and its
`OrganizationInvitation` template, which is also why deleting `internal/mail`
does not remove mail configuration — it moves it upstream.

**What shipped.** `internal/orgsync`: a Management API client over an
in-process `client_credentials` token (refreshed ahead of expiry and on a 401,
paginating on `Link`/`Total-Number`), `ReconcileUser` from the sign-in claims
and `ReconcileOrg` as the job body. Three signals feed it — the
`organizations`/`organization_roles` ID-token claims, a
`Organization.Membership.Updated` webhook at `POST /hooks/logto`, and an
hourly ticker — and **none of them applies a delta**: every one re-reads the
organization and converges, because webhook payloads arrive unordered and
incremental application would be a correctness bug waiting for concurrency.
The webhook is mounted outside `/api/v1` deliberately: that group's auth and
same-origin middlewares are asserted by a router walk, and an unauthenticated
route inside it would mean loosening the test that protects the whole API. It
verifies an HMAC-SHA256 of the **raw** body in constant time, enqueues
`jobs(kind='org_reconcile')` and returns 2xx rather than doing the work inline.

`RemoveMember`'s five steps split in two, which is the other half of this
milestone. **Revocation is immediate** — group rows then the membership row,
on whichever sync path noticed, idempotent — and it is sufficient, because
content access requires current membership, so the grants left standing grant
nothing. **Teardown is confirmed**: `space_departures` queues the destructive
half (grant revocation, the ownership climb, owned roots into the Archive, the
archive grant re-seated) behind `POST /spaces/{id}/departures/{userID}/complete`
and an admin's decision, because an accidental click in the Logto Console must
not be able to trigger something irreversible. `POST /spaces/{id}/members`
takes `{"email", "as_admin"}` and answers **201 added** or **202 invited**,
with 409/422/503 for already-a-member, an unlinked space and an unreachable
IdP; the space DTO gains `admin_count` and `idp_org_id`, and the People page
grows a pending-departures queue and an admin-less banner.

**A space is allowed to end up with no admin**, which is new and deliberate.
pp-hub's own write path still refuses to remove or demote the last one
(`ErrLastAdmin`, checked before the Management API call), but a removal
performed in the Console is a fact, not a request. Auto-promoting
`spaces.owner_id` would invent a privilege nobody asked for and break the rule
that admin means exactly one thing; writing the role back would put pp-hub in
a loop with whoever is editing the Console. So the space says so in a banner
and an instance admin resolves it.

Reconciliation also pre-creates `users` rows from the Management API response,
keyed on `(oidc_iss, oidc_sub)`. That **reverses M28's standing rule** that
nothing creates an account ahead of its owner's first sign-in — right while
pp-hub owned identity, wrong once the IdP is authoritative, since the key is
exact and comes from the system that issued it. Without it the People page
would show a subset of the organization and reproduce the very
two-lists-disagreeing problem this milestone exists to remove.

**Verified.** HMAC verification against a fixed vector — valid, tampered,
missing header, empty body. Convergence against a real Postgres in
`permissions_db_test.go` (skipped without `PP_TEST_DATABASE_URL`): add, remove
and role flip, running reconcile twice changing nothing, and the departure
lifecycle asserting that revocation blocks access immediately while only
`CompleteDeparture` moves content into the Archive. The Management API client
against an `httptest` server covering pagination and a 401 → token refresh;
no live Logto in the suite. And as delivered evidence, a smoke run on
`compose.dev.yml --profile idp`: organization, M2M app and webhook created in
the Console, sign in, the space shows the right members, remove the user
upstream, 403 within one webhook delivery with a pending departure row, then
complete the departure and find the content in the Archive.

**Backfill was considered and refused.** Creating an organization per existing
space and pushing its members up is a real tool, but the project is
pre-production: linking a space to an organization is one `UPDATE` in
development, and reconciling a space whose `idp_org_id` is NULL is a no-op by
design, so an unlinked space is stable rather than emptied. See
[logto-org-sync.md](logto-org-sync.md) for the whole design, and
[deployment.md](../deployment.md) for what an operator must configure in Logto
first.

## M34 — One Settings Dialog ✅ done

No migration, no API change. M16 gave settings an address; it also gave it
two of everything. Account settings were three routes under `/drive/account`
behind a tab strip, space settings three more under `/drive/$spaceId/settings`
behind a second copy of the same strip, and the two were entered from two
different rail menus — the account menu in the footer and the space menu in
the header — so the answer to "where do I change something" depended on which
thing you wanted to change before you could look for it. Both strips consumed
the whole content column, which meant opening Preferences to flip a theme
navigated away from the folder being worked in.

There is now one dialog, and **the hash is what opens it**:
`#settings/general|profile|security|space|members|groups|danger` appended to
whatever page the reader is on. Slugs are flat and globally unique, so parsing
an address is one compare and one lookup and a pane can be regrouped in the
dialog without breaking a pasted link. The invariant M16 paid for is intact —
a pane survives a reload, is sendable to a colleague and answers the back
button — and the history semantics are explicit about it: opening pushes, so
Back closes; switching pane replaces, so Back does not walk out through every
pane that was glanced at.

**The flow is one-way, which is the whole design.** `settings-hash-bridge.tsx`
is the only caller of `SettingsModal.show/hide`, and it reads the hash; every
exit inside the dialog is a navigation that strips the fragment, and the
bridge then hides the modal. The obvious alternative — open imperatively and
write the hash afterwards — has two sources of truth, and Back and reload are
exactly the two cases it gets wrong. The bridge is mounted in the drive shell
rather than the root, so the fragment stays inert on `/login` and `/s/{token}`.

Two things that look incidental are not. The dialog's nav is **plain markup,
not the `Sidebar` primitive**: `ModalContainer` sits at the router root,
outside the drive shell's `SidebarProvider`, so `useSidebar()` would throw —
and nesting a second provider is worse, because it registers a second ⌘B
handler that toggles the rail behind the dialog. And **unsaved edits block the
two dismissals a dialog makes cheap**: Escape and a click on the backdrop do
nothing while a pane is dirty, where the close button and a pane switch ask
first. A page took a deliberate navigation to leave; a dialog does not, so the
guard is new rather than inherited.

Space panes are admin-only, gated by **filtering the pane list** rather than
rendering a refusal: the band disappears, and a deep link to one of its panes
is replaced with the default pane — the same reasoning M16 applied to
`beforeLoad`, at a surface that has no `beforeLoad` of its own. The filter
waits for the spaces query to settle, because treating "not loaded yet" as
"not an admin" would bounce a legitimate deep link before the permission it
needs had been fetched.

Membership picked up a real change on the way. `people-table.tsx` and the
group drawer beside it were joined by `?group=`, which made one search param
mean two things: which group the table filters to, and which group the drawer
is administering. They are now separate panes — Members and Groups — and the
filter is plain component state, because inside a dialog whose address already
names the pane, a filter is a glance rather than a place. `?group=` is gone,
and `group-drawer.tsx` is `group-list.tsx`.

Deleted rather than redirected: ten route files and `settings-tabs.tsx`. A
stale `/drive/account/profile` bookmark now matches `/drive/$spaceId` and
lands on "Space not found" inside the shell, which is an acceptable end state
for an internal tool and cheaper than a redirect table nobody will maintain.
The rail's account menu is the single entry point, opening **upward** from the
footer row it belongs to and carrying the avatar, and the space switcher went
back to switching spaces.

## M25 — Observability ✅ done

Logs and metrics, endpoints only: no tracing, no error tracking, and no
bundled Prometheus or Grafana — an operator points their own at the listener.

**Logging.** `internal/obs.InitLogging` is the first statement of `run()` in
both binaries, ahead of `config.Load` (which already logs), and installs one
`slog` handler for the process: `PP_LOG_LEVEL` (`debug`/`info`/`warn`/`error`,
default `info`) and `PP_LOG_FORMAT` (`text`/`json`, default `text`;
`compose.prod.yml` sets `json`). An unusable value warns and falls back rather
than refusing startup, the same policy as `PP_AUDIT_RETENTION_DAYS`. The
handler adds chi's request id to every record whose context carries one, so
every `slog.*Context` call made while serving a request — and every call site
with an `r` or `ctx` in scope now uses the `Context` variant — joins to the
access log line, to the `X-Request-Id` response header, and to the
`security_events` row that request wrote.

**Access log.** chi's `middleware.Logger` and `Recoverer` are gone.
`httpapi.Server.observe` writes one `"http request"` record per request with
method, redacted path, route pattern, status, bytes, duration, client address
(after `ClientIPFromXFF`, so it is the same address the rate limits and
security events use) and, once `authenticated` has resolved a session, the
`user_id`. It logs at DEBUG for `/healthz` and `/readyz` (compose probes every
ten seconds), ERROR for 5xx, INFO otherwise. Redaction is `redactURI` as
before, and the request is still never rewritten. `recoverer` logs a panic with
its stack through `slog` and answers a problem+json 500.

**Metrics.** A second listener, `PP_METRICS_ADDR` (default `:9091`, `off`
disables), serves only `GET /metrics`. It is its own port so that a routing
mistake in nginx cannot make it public; compose `expose`s it on the internal
networks and publishes nothing. It starts before OIDC discovery, so a server
stuck waiting for its IdP is scrapeable, and a bind failure is logged rather
than fatal. Series:

- `pphub_http_requests_total{method,route,status}`,
  `pphub_http_request_duration_seconds{method,route}` (buckets to 300s:
  downloads stream for minutes) and `pphub_http_requests_in_flight`. `route`
  is always a chi pattern or `unmatched`, never a raw path — ids and share
  tokens must not become label values. When a subrouter middleware answers
  before routing finishes (`authenticated`'s 401), the full pattern is looked
  up in the root router rather than recorded as `/api/v1/*`.
- `pphub_db_pool_*` from `pgxpool.Stat()` every scrape.
- `pphub_jobs{kind,status}` and `pphub_jobs_oldest_age_seconds` (pending,
  running, failed — `done` rows only accumulate), `pphub_blobs{state}` and
  `pphub_blob_bytes{state}` (`referenced`/`unreferenced`, the latter being what
  GC has yet to reclaim), and `pphub_db_stats_up`. These two queries
  (`JobQueueStats`, `BlobTotals`) run at most every 30s with a 2s timeout; a
  failure drops the cached values rather than serving them stale.
- Go runtime and process collectors.

`cmd/worker` has no metrics listener: it has no port by design, and its backlog
is visible from the server through `pphub_jobs`.

## M26 — Audit Completeness ✅ done

Migration `0008_read_audit.sql`. Closes the remainder of P1 "`activity_events`
is a feed, not an audit log" on the `security_events` table M21 added, rather
than a third one: read auditing, an export, and a watcher.

**Reads, deduplicated.** Four new kinds record _intentional_ reads:
`file_downloaded`, `folder_downloaded` (a zip), `file_viewed` (inline
`/content`) and `file_opened` (an editor session, with `detail.mode`). They are
written where the bytes are served — `serveBlob`, `serveShareFile`,
`handleShareDownload`, both `streamZip` callers and `handleEditSession` —
after authorisation and a successful open, before a byte is written, for
signed-in users and share-link visitors alike (a visitor is `subject =
"share_link"`, with `detail.share` and `detail.shared_by`). Thumbnails, WOPI
`GetFile` and avatars are not reads. The volume problem M21 deferred on is
handled by `audit.RecordRead`: one row per (kind, actor, node) per hour, held
in a bounded in-memory map, so Range continuations and a double-click are one
row. The cache is per replica — N replicas can write N rows per window — and
when full it forgets rather than grows, so its failure mode is an extra row,
never a missing one.

**A node column.** `security_events.node_id` (indexed, backfilled from the
`detail.node` that grant and share events already carried) makes "who read
folder X" a filter: `?node=` matches the node and everything under it,
including trashed descendants, and still matches a node purged since. There is
deliberately no foreign key: a purge must not erase the record that someone
read the file. The console joins the node's name, falling back to the name
recorded with the event.

**Retention of its own.** `read_audit_retention_days` (default 90) is a
separate instance setting; the five-minute sweep purges read kinds against it
and everything else against `audit_retention_days`.

**Filters and export.** `GET /api/v1/admin/security-events` takes `?kind=`,
`?node=`, `?from=` and `?to=` (RFC 3339, `to` exclusive; a malformed value is a
400, not a silently wider answer). `GET /api/v1/admin/security-events/export`
streams the same filters as CSV — UTF-8 BOM for Excel, formula-injection
guard on every cell, keyset-paged so a long export neither skips nor repeats
rows — and is itself a `security_log_exported` event. The console gained date
inputs, a click-to-filter file name on each row, and an Export CSV link that
follows the filters on screen.

**Alerting.** `internal/alert` scans the log every minute for addresses with
`alert_failed_login_threshold` (10) failed sign-ins within
`alert_failed_login_window_minutes` (15). Each burst raises one
`security_alert` row per window (subject `login_failed_burst`), increments
`pphub_security_alerts_total{rule}`, and mails every live instance admin
through the existing job queue — no per-person opt-out; a threshold of 0 turns
alerts off. Only one rule ships: `login_failed` rows mostly carry no user, so
a per-account rule would have nothing to key on.

## M36 — Sharing Hardening ✅ done

An audit of the share flow against the model found ways around it; each is
now refused server-side and pinned by a database test
(`internal/httpapi/sharing_db_test.go`,
`internal/spaces/permissions_db_test.go`). Details in
[permissions.md](permissions.md).

- **Moves are bounded** (`authorizeMove`). Ownership inherits, so an editor
  without re-share could drop the owner's file into their own folder and
  come out owning it; and moving someone's file into a team folder took it
  away from them. Non-owners may not move into more access than they hold nor,
  without `CapShare`, change who reaches the node; moving personal content
  into a team folder takes its creator, with no foreign content beneath.
  `move-impact` applies the same rules.
- **Changing or revoking a grant takes covering it**, like creating one: a
  viewer with re-share can no longer strip an editor. Removing the last
  manager of a team folder answers **409**.
- **One direct grant per (node, person)** (`0009_direct_share_uniq.sql`).
  Re-sharing is an upsert; `POST /nodes/{id}/shares` takes `user_id`, not
  `email` (**breaking** for API callers), and answers "unknown" and "not a
  member" identically. `DELETE /nodes/{id}/grants/{groupID}` answers 404 when
  there is nothing to revoke.
- **Links follow their creator's download**, not only their share, and the
  anonymous landing page no longer falls back to the sharer's email.
- **"Shared with me" requires current membership** on its granted branch too.
- **Admins cannot add themselves to a group that opens files**, and every
  group change is a `security_events` row.
- **Multi-row writes are transactions** (`store.Queries.InTx`, savepoints when
  nested): move, team-folder creation with its manager grant, purge, space
  creation, add/revoke membership, completing a departure.
- **The share dialog shows the whole answer**:
  `GET /nodes/{id}/inherited-access` lists owners and ancestor grants; existing
  grants are edited in place; controls the caller does not hold are left out;
  link expiry is a preset and the one-time URL is shown inline.
- `httpapi/permissions_test.go` is gone: it asserted `t.Log` calls and a
  test-local helper, not the product.

## Upcoming — M27 Onward 🚧 not done

Everything above shipped, including the P0 register in full as of M24;
nothing below has except the entries marked done (M30 and M32), and none of
the rest is scheduled — the numbering is the order
the dependencies suggest, which is what
[production-readiness.md](production-readiness.md) says about its own phasing,
and the two follow the same ordering on purpose. Each entry names the section
of that document holding the evidence rather than restating it, so there is
one place to check whether a gap is still real before acting on it.

### Phase B — The Multi-Department Layer

What changes when the user count crosses a couple of departments. Each item is
additive to a pattern that already exists.

#### M27 — Content-Indexed Search 🔜 planned

P1, "Search only matches file names". `GET /api/v1/search` ranks a prefix match
over a mid-string one and falls back to trigram similarity on `nodes.name`;
that is all of it, and it is the difference between finding the contract and
remembering which folder the contract is in — which stops working at about the
point a second department joins. It is additive rather than new
infrastructure: text extraction is a new `jobs.kind` on the DB-backed preview
queue and its optional worker, and office documents already pass through
OnlyOffice conversion on that same path. Finishing it means the job kind, an
index over the extracted text, and a ranking that merges it with the name
match.

#### M28 — Directory Sync 🔜 planned

P1, "Identity is managed by hand". **M33 delivered the membership half of
this**: accounts are pre-provisioned from a Logto organization's member list
and space membership plus admin status arrive from the IdP, so the rule this
entry used to state — nothing creates an account ahead of its owner's first
sign-in — no longer holds, and neither does re-entering the company roster by
hand. What is left is the rest of the directory: no SCIM, no admin
create-user, and **no group sync** — a Logto organization template is
tenant-wide and cannot express a per-space, user-created group, so department
structure below the company line is still built in pp-hub and kept in step by
hand. Deprovisioning is the harder half and M33 only moved it: a departure is
revoked immediately and torn down per space on an admin's confirmation, but
there is still no `DeleteUser`, suspension is still the terminal state, and
there is no instance-level equivalent of the archive hand-off.

### Phase C — Collaboration and Storage Honesty

#### M29 — Comments and Notifications 🔜 planned

P1, "No comments", plus the notification half of P0 §2, and it is now the
whole of it. **M23 built a transport and M33 deleted it**: `internal/mail`
existed only to send the space invitation, invitation email moved to Logto's
own connector, and the package and its `PP_SMTP_*` variables went with it —
so there is again no mailer, alongside the `notifications` table, route, SSE
or websocket that never existed. A comment nobody is told about is still a
comment nobody answers. Comments themselves are a whole subsystem — a table,
endpoints and two UIs, the preview modal and the editor — and have been
deferred in those words at every milestone since M10, which was accurate
each time. Finishing it means all of it: threads on a node, a notification
story, and a transport chosen again — a relay of our own, or the IdP's
connector if Logto's templating turns out to stretch that far.

#### M30 — Resumable Upload and Honest Accounting ✅ done

P1, "Uploads are not resumable" and "Storage accounting understates real
usage". The `uploads` table is in the baseline and no Go code references it —
dead schema — so the transfer tray's pause stops the queue rather than a
transfer in flight, a large upload that drops near the end restarts at zero,
and nothing sweeps abandoned rows because nothing writes them. The accounting
half is worse than incomplete: `node_versions` grows without bound with
nothing pruning it while quota counts only the current version, so the two
numbers diverge over time and the disk fills while the meter says there is
room. Finishing it means chunked upload against that table with a sweep for
what is abandoned, and either a quota that counts every referenced blob or a
version retention policy — ideally both. Per-user quota, file-count limits and
egress accounting are named in the same section and are not part of this.

What made it urgent was a production incident rather than the P1 list.
Behind Dokploy's Traefik (a 60-second `readTimeout` on the whole request) and
Cloudflare (a 100 MB body cap), one multipart POST failed for any file whose
upload took longer than a minute — the incident is written up under Dokploy
in [deployment.md](../deployment.md). Raising the timeout fixed that one
proxy; it could not lift Cloudflare's cap, and every bound on request
duration is shorter than some uplink's time to send some file. The durable
answer is requests that stay short however big the file is.

**Decision taken: tus 1.0.0, and no multipart path beside it.** The protocol
already specifies what a bespoke chunk API would have had to invent — where
an upload stands (HEAD), what a mismatched offset answers (409 with the
server's offset), how an abandoned upload is let go (DELETE) — and a second
route for file content would be a second place to enforce the cap, the
quota and the editor lock. So `POST /api/v1/spaces/{spaceID}/files`
(`handleUpload`) and `POST /api/v1/nodes/{nodeID}/versions`
(`handleUploadVersion`) are **gone**; `GET /nodes/{id}/versions` and
`POST /versions/{versionID}/restore` stay. Only the core protocol plus the
creation and termination extensions are implemented, and
`Upload-Defer-Length` is refused on purpose: every ceiling below needs the
length up front.

**What shipped — the upload.** `internal/httpapi/uploads.go`, inside the
authenticated, same-origin `/api/v1` group:

- `POST /api/v1/uploads` with `Upload-Length` and `Upload-Metadata`
  (`filename`, `space_id`, `parent_id` for a new file; `node_id` alone to
  replace an existing file with a new version). The declared length is
  checked against the upload cap (`PP_MAX_UPLOAD_BYTES`, or the admin
  `max_upload_bytes`) and against the space's quota — counting the bytes
  other uploads into the space have already promised
  (`PendingUploadBytes`), so ten parallel uploads cannot each pass a check
  the ten together would fail. Both answer **413** before a byte is
  accepted. Authorization is membership plus parent write for a new file,
  `CapEdit` on the node for a replacement, plus the M24 editor-lock check —
  answering **423 Locked** here, because tus reserves 409 for an offset
  mismatch (`lockedByEditor` now takes the status; restore-version still
  answers 409).
- The POST may carry the first chunk (tus **creation-with-upload**:
  `Content-Type: application/offset+octet-stream`). A small file then lands
  in that one request — a folder of photos would otherwise spend twice the
  requests it did before M30 against the per-IP API rate limit. `Location`
  is set before a byte is read, so even a POST whose body broke tells the
  client where to resume.
- `HEAD /api/v1/uploads/{uploadID}` reports the offset;
  `PATCH /api/v1/uploads/{uploadID}` (`application/offset+octet-stream`,
  `Upload-Offset`) appends; `DELETE` abandons. An upload is visible only to
  the account that created it — anyone else gets 404.
- **An interrupted PATCH keeps what arrived.** `uploads.Service.Append` runs
  on a context detached from the request, copies through an
  `io.LimitReader` bounded by the declared length, and fsyncs the staging
  file before the row's offset advances, so a crash can lose the tail of a
  chunk but never leave the row ahead of the file. A staging file shorter
  than the recorded offset — a database restored without the staging
  directory — is discarded rather than resumed, because resuming would
  splice zeros into the file.
- **The final PATCH commits before it answers**, so a 204 at full length
  means the file is in place. Authorization and the lock are checked again
  at commit; a lock or a 5xx keeps the upload (the client retries with an
  empty PATCH at `offset == length`), while 403/404/413 drop it rather than
  hold disk and quota until it expires. A committed row stays until it
  expires so a client that lost the final response learns from HEAD that
  the file landed, and a repeated final PATCH is idempotent. A per-upload
  in-process lock serialises PATCHes — which, with staging on local disk,
  is the single-server-process assumption written down.
- `internal/uploads` owns the rows and the staging files, under
  `config.UploadDir()` = `<PP_BLOB_ROOT>/.uploads` for both blob backends:
  the one path every deployment already persists, and with the local
  backend the finished file is renamed into the store by the new
  `blob.Store.Ingest` instead of copied (S3 hashes, puts, then removes).
  An upload lives 24 hours **of idleness** — every accepted chunk slides the
  expiry — and `uploads.Service.Run` sweeps every 15 minutes: expired rows,
  then their files, then staging files untouched for TTL + 1 h that belong
  to no row. The backup script excludes `.uploads`.
- Migration `0010_resumable_uploads.sql` reshapes the table nothing had ever
  written: `mime` dropped (derived from the name at commit, as it always
  was), `total_size NOT NULL`, `node_id` (FK, `ON DELETE CASCADE`),
  `completed_at`, a `0 ≤ uploaded ≤ total_size` CHECK, and the sweep and
  pending-bytes indexes.
- Web: `lib/api/tus.ts`, a hand-written client. Chunks start at 4 MiB and
  follow measured throughput toward 15 s per PATCH, clamped to 256 KiB–64 MiB.
  After a failure the chunk is halved, and once the HEAD resync says how much
  of the cut chunk actually arrived, sized from that rate instead — a proxy
  that timed the request out still passed on what it had read, so one
  timeout is enough to get under its deadline. Files up to 256 KiB (and
  empty ones) ride on the creation POST (tus creation-with-upload), one
  request per file, as before M30. Network errors, 408, 429, 499, 5xx and
  Cloudflare's 52x are retried with backoff and a HEAD resync, and a failure
  that still moved the offset resets the retry count. `lib/upload-store.ts`
  keeps each transfer's upload URL, so the tray's **pause now stops
  transfers in flight**, resume and retry continue from the server's offset,
  and cancel sends DELETE. The URL is held in memory only, so a page reload
  starts the file again and the abandoned upload expires.

**What shipped — the accounting.** `SumSpaceUsage` now counts **every stored
version of every file in the space, trash included** — what occupies the
disk — and the migration recomputed every space's `used_bytes` under that
definition. `fs.EnsureRoom` replaces `checkQuota` and compares used plus
incoming against the quota, so a new version costs its full size: the
version it supersedes is still on disk. The check still runs after the bytes
land — the declared length is a courtesy refusal at creation, not the
enforcement — but now **before the node is created**, so a refused upload no
longer leaves an empty file behind in the folder. `fs.PutFile` and the new
`fs.PutStagedFile` recount usage themselves, which also makes WOPI saves
move the meter. The other half is **version retention**:
`version_retention_count` in Admin › Settings (default 100, Google Drive's
figure; 0 keeps everything; 0–10000; applies without a restart). After every
new version `fs.pruneVersions` deletes what lies beyond the newest N — the
current version always survives and counts as one — and releases each
pruned blob to GC; previews cascade. It applies on each file's next save, not
in a retroactive sweep. `GET /api/v1/usage` gains `versions` and `trash`
slices so the breakdown sums to the meter, and the web storage breakdown
shows them as "Older versions" and "Trash".

`TestInterruptedChunkResumesFromTheBytesThatArrived`,
`TestReplacementUploadHonoursTheEditorLock`,
`TestUploadCreationCountsHistoryAndUploadsInFlight`,
`TestVersionRetentionKeepsTheNewestAndReleasesTheRest` and
`TestUsageBreakdownAddsUpToTheMeter` hold it against a real Postgres, with
the sweep and the row-ahead-of-file case in `internal/uploads`.

Deliberately out, as the entry said: per-user quota, file-count limits and
egress accounting — still open under the same section of
[production-readiness.md](production-readiness.md). Usage sums version
sizes rather than distinct blobs, so a copy (which shares its blob) counts
twice: the meter bounds what a space holds, not the deduplicated disk.

#### M31 — Document Creation 🔜 planned

P1, "No way to create a document". There is no blank-document template and no
create-office-file path anywhere in `apps`, so every document has to be
created somewhere else and uploaded before it can be co-edited — which leaves
a real co-editing stack working as a viewer with write access rather than a
place work starts. Finishing it means templates plus a create path that lands
an empty document in the write target M19 resolves and opens the editor on it.

#### M32 — The UI Speaks More Than One Language ✅ done

Named for years under "Named but Unscheduled" as _"The UI is English-only. No
i18n library, no locale files, no string catalogue in `apps/web/src`"_, which
is no longer true. The SPA ships English and Vietnamese through Lingui, with
gettext `.po` catalogs in `apps/web/src/locales/` that
`@lingui/vite-plugin` compiles into the bundle — so there is no runtime ICU
parser and no catalog fetch.

Three decisions carry the design. **Message ids are hashed from the source
text**, so migrating ~900 strings already written inline in JSX cost no
key-naming at all, and `@lingui/format-po` still writes the English as
`msgid`, so a Weblate translator reads `New folder` rather than `MbT6FE`.
**Language is an account preference, not a device one** — it is a fact about
a person — so it lives on the `users.settings` JSONB column that has existed
since `0001_init.sql` and is read off `GET /users/me`, with
`PATCH /api/v1/users/me/preferences` merging partial writes so a client that
knows one preference cannot blank the rest. Because the account answer
arrives one round trip after first paint, every applied locale is mirrored
into `localStorage`, and that echo — not the account — is what the first
frame reads; without it every returning Vietnamese user would see a flash of
English. **Module-scope label maps hold `MessageDescriptor`s, never rendered
text**: a constant evaluated at import time freezes whichever catalog loaded
first, and `lingui/t-call-in-function` is an error so nobody rediscovers
that. Sentences that used to be spliced from fragments — the activity feed's
`{actor} {verb} {node} in {space}`, the selection summary's "2 folders and 1
file" — are now single ICU messages, because word order is not English's to
lend.

The OnlyOffice ribbon follows through `?ui=<locale>` on the edit session,
resolved into the Document Server's `editorConfig.lang`. Translation runs in
a self-hosted Weblate (GPL-3, no seat cap), which is provisioned rather than
merely recommended: `deploy/weblate/compose.yml` stands it up outside the
product stack, and `deploy/weblate/pp-weblate-setup.sh` creates the project
and component over the REST API, so the settings that are easy to get wrong
and invisible once wrong — bilingual `po` rather than `po-mono`, an empty
`template`, the `icu-message-format` flag — live in git instead of in a web
form. Standing it up against this commit confirmed the catalog independently:
677 of 677 Vietnamese messages, zero failing checks. And
`apps/web/tests/catalogs.test.ts` holds the invariant Weblate cannot: that a
catalog arriving by any other route still has balanced ICU braces and the
placeholder set of its source.

Deliberately out: **the server is still English only** — problem+json details
and `/login?error=` codes, and now also whatever Logto's own invitation
template says — though the client already
turns those codes into translated sentences, which is the pattern to extend.
See [i18n.md](i18n.md).

### Named but Unscheduled

Named in P1's "Smaller, but on the list" and assigned to no milestone. They
are recorded here so they are found rather than rediscovered:

- **`style-src 'unsafe-inline'`** is the one CSP directive in the nginx
  template that is looser than it should be rather than looser on purpose.
  Removing it takes changes in `apps/web` — the inline `style` props a few
  components rely on — not an edit at the edge.
- **No orphan-blob audit.** `gc.go` deletes the blob row before the file on
  purpose, so a crash between them leaks a findable file rather than leaving a
  row that promises bytes which are gone, and it defers that leak to "the next
  full audit" — which does not exist.
- **No antivirus or content scanning.** Uploaded bytes go from the upload's
  staging file (the multipart part, before M30) straight into the CAS with no
  scan hook, no sniffing (the type is guessed from the extension) and no
  extension blocklist.
- **No encryption at rest.** Local blobs are plaintext, the S3 backend sets no
  SSE headers, and Postgres runs `sslmode=disable` inside the compose network.
  Share tokens and session tokens _are_ stored hashed.

## Testing Strategy

- **Unit**: capability resolver matrix (ownership × grant × ancestor × link), blob CAS (put/get/dedup/gc), tree ops (move-cycle rejection, rename conflicts).
- **Database-backed**: the invariants that live in SQL — owner inheritance, admin-has-no-access, member lifecycle, organization convergence and the departure queue, session lookup and revocation, and WOPI access re-resolved against live grants — run against a real Postgres and skip without `PP_TEST_DATABASE_URL`.
- **Structural**: three router walks. `TestEveryAPIRouteIsAuthenticated` fails if an `/api/v1` route is not wrapped by the auth middleware, `TestEveryAPIMutationChecksOrigin` fails if a non-safe `/api/v1` method is not wrapped by `sameOrigin`, and `TestWopiIsExemptFromOriginChecks` fails if the WOPI group ever is. A convention nobody can forget is worth more than a convention everybody is told about.
- **Integration** (compose test profile): upload→download byte-identical roundtrip; WOPI flow against real OnlyOffice (open → PutFile → version increments); link share access without auth.
- **Contract**: hand-typed TS interfaces in `apps/web/src/lib/api/types.ts` mirror the Go handlers; OpenAPI codegen deferred until the surface grows.
- **Load sanity**: k6 — 100 concurrent uploads, 5-user co-edit; pass = no errors, no performance target yet.

## Rollout / Operations

- Two compose files in `deploy/compose/`: `compose.dev.yml` (services only, app runs from source) and `compose.prod.yml` (everything, for a VPS or Dokploy). Config via `.env`, templated by `.env.example` in each location.
- Persistent state: `data/` volume (blobs, plus in-flight resumable uploads under `.uploads`, which backups skip) + Postgres volume.
- Backup: `deploy/backup/pp-backup.sh` and `pp-restore.sh` — **implemented in
  M22**, dumping Postgres before archiving blobs (never the reverse), self-
  verifying, and driven entirely through `docker compose run`/`exec` so no
  volume name is ever hardcoded. See
  [production-readiness.md](production-readiness.md), which collects
  everything that stood between M21 and an internal rollout: the P0
  register, now fully closed (M22–M24 closed backup, mail transport and lock
  enforcement on the REST version-upload path; the session/cookie,
  OnlyOffice JWT, admin bootstrap and upload/rate-limit items were closed in
  M20–M21, as were the sweep findings in its §8, and the readiness split and
  avatar-upload stub closed alongside M22–M24), the P1 gaps against Google
  Drive still open (content search, comments, IdP group sync — M27 through
  M31; resumable upload and storage accounting closed in M30), and a
  proposed phasing, whose order Upcoming follows.
