# Use Cases

## Actors

- **User** — authenticated via OIDC against Logto, the only supported identity provider; which spaces they belong to is their Logto organizations.
- **Space member** — belongs to a space. Membership alone opens no file: what they reach is what they created plus what has been granted to them or to a group they are in.
- **Space admin** — member of the space's automatic `Admin` group. Runs membership, groups and space settings; gains no file access from it.
- **Anonymous visitor** — holds a share link token, no account.

## Drive

### UC-1: Upload a file

1. User opens a space, navigates to a folder, uploads a file. The browser sends it as a resumable tus upload (`POST /api/v1/uploads`, then `PATCH` chunks), so a dropped connection resumes from the last byte the server kept instead of from zero, and pause/resume in the transfer tray act on the transfer in flight.
2. The declared size is checked against the upload cap and the space quota before any bytes move; when the last chunk lands, the server hashes the staged file into the CAS blob store (`Ingest` → blake3 hash), checks the quota again against the measured size, and commits a `node_version`.
3. Duplicate content reuses the existing blob (dedup via content hash).
4. Replacing an existing file is the same upload naming the file: the bytes become its new version, older versions beyond the instance's retention count (default 100) are pruned, and the whole history counts against the space quota.

### UC-2: Organize folders

- Create/rename/move/copy/delete folders and files within a space.
- Name uniqueness is enforced per parent folder (among non-deleted nodes).
- Move rejects cycles (cannot move a folder into its own descendant).

### UC-3: Restore a previous version

1. User opens version history of a file (immutable `node_versions` list).
2. Restore flips `current_version_id` to the chosen version; no blob is rewritten.

## Sharing

### UC-4: Share to a person or a group

1. Whoever can share the node — its creator, or someone whose grant carries the re-share flag — grants it to a space member (`POST /nodes/{nodeID}/shares`, `kind: "direct"`) or to one of the space's groups (`PUT /nodes/{nodeID}/grants`).
2. The request carries the capability itself: a level (`view` | `edit`) plus the independent `can_download` and `can_share` flags, so "view but pass it on" and "edit but do not re-share" are both expressible. `edit` implies `can_download`, because an editor must be able to fetch what it edits.
3. The grant inherits down the whole subtree under the node it names, so granting a folder opens everything inside it.
4. A sharer can never hand out more than they hold themselves (403), and a grantee outside the space is refused with "share a link instead".

### UC-5: Share link

1. Whoever can share the node creates a link with an optional expiry. A link is always view-level and never re-shares; the only choice is whether raw download is allowed.
2. Server stores only `token_hash` (sha256); the plaintext token is shown once.
3. Anonymous visitor resolves the link → view, plus download when the link allows it (edit via link is post-MVP). Every node id an anonymous caller supplies is walked back up to the share root first, so a token reaches its own subtree and nothing else.
4. Revoke = delete the share row.

## Collaborative Office

### UC-6: Edit a document in browser

1. User clicks Edit on a .docx/.xlsx/.pptx in the Drive UI → the editor opens full-page in a new tab at `/edit/{nodeId}`.
2. Server issues a short-lived HMAC-signed WOPI access token (`user_id`, `node_id`, and whether the caller resolved to `edit`) and returns the OnlyOffice 9.x action URL; the SPA POSTs it (with `access_token`/`access_token_ttl`) into the editor iframe.
3. OnlyOffice calls WOPI host endpoints: `CheckFileInfo`, `GetFile` (streams current version blob), `Lock/Unlock/RefreshLock`.
4. On save, OnlyOffice `PutFile`s the new content → server writes a new blob and appends a new `node_version`.

### UC-7: Realtime co-editing

- Multiple users open the same file → same WOPI `FileId` → OnlyOffice Document Server merges edits in realtime.
- Server receives the merged result via `PutFile`; a version conflict (`Version` mismatch) returns 409 and OnlyOffice re-merges.

## Permissions

### UC-8: Space membership

1. A space admin adds a member by email, optionally as an admin, and promotes or demotes one through `POST /spaces/{spaceID}/members/{userID}/set-admin`.
2. Every space has an automatic `Everyone` group (its membership is the space's membership) and an `Admin` group; neither can be renamed, deleted or hand-edited through the groups API.
3. A new member sees nothing but what they create: any member may create at their own top level, and what lands there is private to them until they share it. Writing inside someone else's folder needs `edit` on that folder.
4. Removing a member revokes every grant they hold and every grant they issued, moves ownership of their nodes nested under somebody else's folder up to the nearest ancestor's creator, then re-parents what remains under the space's `Archive` folder (`" (n)"` suffix on a name clash) and drops them from every group. Nothing is deleted — the `Admin` group's grant on `Archive` is the one place an admin reads content it does not own. With the membership row gone the departed person reaches nothing, not even what they created; re-adding them restores that ownership.
5. Removing or demoting the last admin is refused (409); a space with no admin has no route back through the UI.

### UC-9: Effective permission check

- Every node access resolves in one recursive query, and it starts with membership: a caller who is not in the node's space resolves to nothing. Inside the space, owning the node or any ancestor (`created_by`) is intrinsic full access; otherwise capabilities are the union of the unexpired grants on the node and its ancestors — view, download, share, edit. Grants only widen and nothing narrows; there is no baseline access, so an admin who owns nothing and was granted nothing resolves to nothing.
- Each route names the capability it needs; node listings carry the resolved set as `caps`, so the UI enables exactly the actions the server would allow.
- A link token (no user) resolves to view, plus download if the link allows it — read paths only.

## Previews

### UC-10: Preview without download

1. On upload, a background job generates a preview: a 256px thumbnail for images, PDFs, video and office documents, or a 4KB snippet for text.
2. Preview is stored as a normal blob, mapped in `previews(node_version_id, kind, blob_hash)`.
3. UI requests the preview blob; full file is never transferred. Cached for a
   year against a version-keyed URL, so a thumbnail is fetched once per version.
4. PDF, video and office rendering needs poppler/ffmpeg/OnlyOffice and runs in
   an optional worker. Without it those files fall back to their type icon
   rather than erroring — deploying the worker later backfills them.
