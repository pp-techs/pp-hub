# Delivery Milestones

Each milestone ends in a working end-to-end state inside the compose stack.

## M1 — Skeleton ✅ done

- `apps/server` boots; chi router; OIDC login against **Logto** (cloud or self-hosted in compose); `GET /me`.
- Website calls the API (same-origin via Vite proxy; OpenAPI-generated TS client deferred until the API surface grows in M2).
- Verified: `go test ./...` (session cookie unit tests), `vp check`, OIDC discovery and login flow.
- IdP note: Logto là IdP (Cloud tenant mặc định cho dev, hoặc self-host qua `compose.logto.yml`); backend dùng standard OIDC. Setup steps and compose file variants live in [deploy/compose/README.md](../../deploy/compose/README.md) (main dev stack: `compose.pg-oo.yml`).

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
- `preview.Worker` claims with `FOR UPDATE SKIP LOCKED`, processes thumbnail (image/png,jpeg,gif,webp → PNG 256px), PDF (placeholder), video (probe placeholder), text (first 4KB).
- Hook: `fs.SetVersionHook` enqueues a preview job after every `PutFile` commit.
- `GET /api/v1/nodes/{id}/preview?kind=…` serves the preview blob (image/png or text/plain).
- Website: 👁 button per file → inline preview (img for images, pre for text).
- Verified E2E: upload PNG → thumbnail 200 (74B PNG); upload text → text preview 200 with content.
- Deferred: real PDF rasterization (pdfium/pdftoppm) and video frame extraction (ffmpeg) — placeholders in place, job kinds registered.
- Expired-lock sweep runs every 5m in-process.

## Testing Strategy

- **Unit**: permission resolver matrix (role × share × ancestor), blob CAS (put/get/dedup/gc), tree ops (move-cycle rejection, rename conflicts).
- **Integration** (compose test profile): upload→download byte-identical roundtrip; WOPI flow against real OnlyOffice (open → PutFile → version increments); link share access without auth.
- **Contract**: hand-typed TS interfaces in `apps/web/src/lib/api/types.ts` mirror the Go handlers; OpenAPI codegen deferred until the surface grows.
- **Load sanity**: k6 — 100 concurrent uploads, 5-user co-edit; pass = no errors, no performance target yet.

## Rollout / Operations

- Compose files live in `deploy/compose/` — main dev stack (pg+onlyoffice, use with Logto Cloud), self-hosted Logto variant, onlyoffice-only; configuration via env vars (`.env.example`, `server.env`).
- Persistent state: `data/` volume (blobs) + Postgres volume.
- Backup: `pg_dump` + rsync of `data/blobs`.
