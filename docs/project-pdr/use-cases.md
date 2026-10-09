# Use Cases

## Actors

- **User** — authenticated via OIDC against Logto, the only supported identity provider; which spaces they belong to is their Logto organizations.
- **Space member** — belongs to a space. Membership alone opens no file: what they reach is what they created plus what has been granted to them or to a group they are in.
- **Space admin** — member of the space's automatic `Admin` group. Runs membership, groups and space settings; gains no file access from it.
- **Anonymous visitor** — holds a share link token, no account.
- **Outsider** — has an account but is not a member of a node's space; reaches exactly what was shared with them by email (an external grant), when the instance and that space allow it.

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
4. A sharer can never hand out more than they hold themselves (403). A user id outside the space is refused (422); somebody outside is reached by email instead (UC-15).

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

- Every node access resolves in one recursive query, and it starts with membership. Inside the space, owning the node or any ancestor (`created_by`) is intrinsic full access; otherwise capabilities are the union of the unexpired grants on the node and its ancestors — view, download, share, edit. Outside it, only external direct grants count, while the instance and the space allow external sharing — no group grant, no ownership (UC-15). Grants only widen and nothing narrows; there is no baseline access, so an admin who owns nothing and was granted nothing resolves to nothing.
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

## Block Documents

### UC-11: Write a document together

1. **New → Document** creates a `.md` file in the current folder, blank or from a template (meeting notes, outline, project spec, runbook, weekly report — in English or Vietnamese).
2. `/edit/<id>` opens it in the block editor; everyone with edit access types into the same document live, with presence avatars and remote carets. Readers who may download it follow along read-only.
3. Images, videos and files dropped in become attachments of the document; an embed block turns a URL into a bookmark with the page’s title and image, fetched by the server.
4. The room writes ordinary versions; a version can be previewed rendered and restored.
5. **Export with images (.zip)** takes the markdown out with its assets; a share link that allows download shows the document rendered, read-only.

### UC-12: Discuss a document

1. Anyone who can open the document selects text and comments on it; others reply, and the thread's opener or an editor resolves it.
2. `@name` in a comment, or a mention written into the document itself, notifies that person — only if they can open the document.
3. The document's owner hears about new threads, participants about replies. The bell in the header shows the unread count, updated live; clicking an item opens the document on the thread. There is no email for comments.

### UC-16: Organise a document into pages

1. In a document, `/page` creates a sub-page — a document nested under it — and opens it; its title is its name, and renaming it updates the block in the parent. A page can carry an emoji icon and a cover image, set from the page header.
2. The editor shows the page tree beside the document and a breadcrumb above it; the Drive listing shows only the top-level document, with a badge counting its sub-pages.
3. `@` or `/link to page` inserts a link to any page in the space, showing its current title; under each page, "Linked from" lists the pages that link to it, limited to those the reader can open.
4. Sharing a document shares its sub-pages; a share link can navigate into them. Deleting a sub-page block moves the page to the trash, and undo brings it back.
5. Export, download and copy take the sub-pages along: the zip holds `Doc.md` beside a `Doc/` folder of sub-pages, with links between them made relative.

## Search

### UC-13: Find a file by what it says

1. The ⌘K palette matches names across every space the user belongs to, and — for markdown and plain-text files — their contents, accent-insensitively.
2. A content match shows a snippet with the matched words highlighted; name matches rank first. Results are filtered to what the user can reach, as for name search. Office documents and PDFs are matched by name only.

## Personal Use

### UC-14: A personal space

1. An instance admin sets `personal_spaces` in Admin › Settings › Personal use: `members` (people in at least one organization) or `everyone` (any account; needs a personal quota).
2. At sign-in the account gets its personal space — no organization, the owner its only member — listed first in the switcher as "My Drive", alongside the organization spaces.
3. It has General settings only: no members, groups, departures, team folders, linking or deletion (409), and its quota is the instance's.
4. When the account is deleted in Logto, the space is archived; an instance admin purges it from Admin › Settings.

### UC-15: Share with somebody outside the space

1. With `external_sharing_enabled` on the instance and "Share outside this space" on the space (always on for a personal one), the share dialog accepts a typed email.
2. An account outside the space gets an external grant (view or edit, never re-share) and a mail linking to `/open/{id}`; an address with no account gets an invitation (202) that becomes a grant at its first verified sign-in. Fifty new outsiders per sender per day.
3. The outsider finds it under **Shared from outside**, browses it by node, opens and co-edits documents, and mentions only the people the document was opened to. Either switch off ends their access at once.

## Agents

### UC-17: Work on space files through a mount

1. An instance admin switches on **Admin › Settings › WebDAV** (`webdav_enabled`, off by default).
2. A person mints a WebDAV token in **Settings › Security › Mount as a drive** and mounts `${PP_EXTERNAL_URL}/dav/` with rclone (or Finder, Explorer), the token as password.
3. A coding agent on their laptop runs local tools (pandoc, python, a script) over the mounted files, with exactly the person's access.
4. Each save is a new version of the same file; a save that raced someone else's lands beside it as a conflicted copy; a delete goes to the trash. See [webdav.md](../architecture/webdav.md).
