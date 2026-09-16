# Use Cases

## Actors

- **User** — authenticated via OIDC (Logto in dev; any OIDC provider works).
- **Space owner / editor / viewer** — user with a role inside a space.
- **Anonymous visitor** — holds a share link token, no account.

## Drive

### UC-1: Upload a file

1. User opens a space, navigates to a folder, uploads a file (multipart or resumable tus).
2. Server streams content into the CAS blob store (`Put` → blake3 hash), then commits a `node_version` in a transaction.
3. Duplicate content reuses the existing blob (dedup via content hash).

### UC-2: Organize folders

- Create/rename/move/copy/delete folders and files within a space.
- Name uniqueness is enforced per parent folder (among non-deleted nodes).
- Move rejects cycles (cannot move a folder into its own descendant).

### UC-3: Restore a previous version

1. User opens version history of a file (immutable `node_versions` list).
2. Restore flips `current_version_id` to the chosen version; no blob is rewritten.

## Sharing

### UC-4: Direct share to a teammate

1. Owner/editor shares a file or folder to a specific user with a permission (`view` | `download` | `edit`).
2. Permission resolver: base = space role; direct grant on the node or nearest ancestor raises the effective permission (shares only widen, never narrow).

### UC-5: Share link

1. User creates a link share with a permission and optional expiry.
2. Server stores only `token_hash` (sha256); the plaintext token is shown once.
3. Anonymous visitor resolves the link → view/download only (edit via link is post-MVP).
4. Revoke = delete the share row.

## Collaborative Office

### UC-6: Edit a document in browser

1. User clicks Edit on a .docx/.xlsx/.pptx in the Drive UI → the editor opens full-page in a new tab at `/edit/{nodeId}`.
2. Server issues a short-lived HMAC-signed WOPI access token (`user_id`, `node_id`, `perm`) and returns the OnlyOffice 9.x action URL; the SPA POSTs it (with `access_token`/`access_token_ttl`) into the editor iframe.
3. OnlyOffice calls WOPI host endpoints: `CheckFileInfo`, `GetFile` (streams current version blob), `Lock/Unlock/RefreshLock`.
4. On save, OnlyOffice `PutFile`s the new content → server writes a new blob and appends a new `node_version`.

### UC-7: Realtime co-editing

- Multiple users open the same file → same WOPI `FileId` → OnlyOffice Document Server merges edits in realtime.
- Server receives the merged result via `PutFile`; a version conflict (`Version` mismatch) returns 409 and OnlyOffice re-merges.

## Permissions

### UC-8: Space membership

1. Space owner invites a user with role editor or viewer.
2. Viewer: read/download. Editor: read/write/upload/edit. Owner: editor + manage members + space settings.

### UC-9: Effective permission check

- Every node access resolves: (1) space role → base; (2) direct share on node/ancestor → widen; (3) link token (no user) → link permission, read paths only.

## Previews

### UC-10: Preview without download

1. On upload, a background job generates a preview: a 256px thumbnail for images, PDFs, video and office documents, or a 4KB snippet for text.
2. Preview is stored as a normal blob, mapped in `previews(node_version_id, kind, blob_hash)`.
3. UI requests the preview blob; full file is never transferred. Cached for a
   year against a version-keyed URL, so a thumbnail is fetched once per version.
4. PDF, video and office rendering needs poppler/ffmpeg/OnlyOffice and runs in
   an optional worker. Without it those files fall back to their type icon
   rather than erroring — deploying the worker later backfills them.
