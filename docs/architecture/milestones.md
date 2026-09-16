# Delivery Milestones

Each milestone ends in a working end-to-end state inside the compose stack.

## M1 — Skeleton ✅ done

- `apps/server` boots; chi router; OIDC login against **Logto** (cloud or self-hosted in compose); `GET /me`.
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
- Web: `/drive/home`, `/drive/recent`, `/drive/starred`; `cross-space-table.tsx`, `activity-feed.tsx`, `storage-breakdown.tsx`; star in the node menus; Home / Recent / Starred in Quick Access.
- `/drive` still redirects to the last space opened — making Home the landing surface is a one-line change to the redirect in `drive.tsx`, deliberately not taken unilaterally.

## M10 — Space Settings & Admin Console ✅ done

- Migration `0009_space_settings.sql`: `spaces` gains slug / description / default_role / archived_at; `space_invitations`; `users.suspended_at`.
- **Invitations exist because `space_members.user_id` references a user row.** An invited address may have no account, so the promise of membership cannot live in that table. Only the token hash is stored; the plaintext is shown once, like a share link. Re-inviting the same address **replaces** the pending row (partial unique index on `(space_id, lower(email)) WHERE accepted_at IS NULL`), so only the newest link works.
- Inviting an address that already has an account adds them as a member directly — an invitation would be a pointless extra step for someone who can already sign in.
- Accepting checks that the invitation's address matches the signed-in account: it is addressed to an email, not to whoever holds the link.
- `PATCH|DELETE /api/v1/spaces/{id}`, `GET|POST /spaces/{id}/invitations`, `DELETE /spaces/{id}/invitations/{inviteID}`, `POST /invitations/accept`, `GET /admin/stats`, `PATCH /admin/users/{userID}`.
- **Suspension is enforced in `requireUser`**, not at sign-in, so it takes effect on the next request rather than waiting for an existing session to expire. 403 and not 401: the credentials are fine, the account is not. An admin cannot revoke their own admin flag or suspend themselves — that would leave the server potentially unadministrable with no way back through the UI.
- Web: `?panel=settings` with General / Members / Danger zone tabs, `/invite/$token`, and a rewritten admin console with stat tiles, suspend/reinstate and admin toggles.

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

## Testing Strategy

- **Unit**: permission resolver matrix (role × share × ancestor), blob CAS (put/get/dedup/gc), tree ops (move-cycle rejection, rename conflicts).
- **Integration** (compose test profile): upload→download byte-identical roundtrip; WOPI flow against real OnlyOffice (open → PutFile → version increments); link share access without auth.
- **Contract**: hand-typed TS interfaces in `apps/web/src/lib/api/types.ts` mirror the Go handlers; OpenAPI codegen deferred until the surface grows.
- **Load sanity**: k6 — 100 concurrent uploads, 5-user co-edit; pass = no errors, no performance target yet.

## Rollout / Operations

- Two compose files in `deploy/compose/`: `compose.dev.yml` (services only, app runs from source) and `compose.prod.yml` (everything, for a VPS or Dokploy). Config via `.env`, templated by `.env.example` in each location.
- Persistent state: `data/` volume (blobs) + Postgres volume.
- Backup: `pg_dump` + rsync of `data/blobs`.
