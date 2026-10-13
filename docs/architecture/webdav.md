# WebDAV Access

A person mounts their drive at `${PP_EXTERNAL_URL}/dav/` in Finder, Explorer or
rclone and works on files as files. It is there so a coding agent on their machine
(Claude Code, Cursor, Codex) can run local tools such as pandoc, python, officecli
or a script over the company's documents. The data stays in one place, the server.
A mount is another view of the same nodes, versions and grants, not a local copy
that can drift.

Code: `apps/server/internal/httpapi/dav.go` (endpoint, auth, reads, DELETE,
MKCOL), `dav_tree.go` (namespace, listings, the x/net/webdav adapters),
`dav_write.go` (PUT, COPY, MOVE), `dav_state.go` (what is remembered between
requests), `dav_names.go` (scratch names, labels). Tests:
`dav_db_test.go`.

## Decisions

- **WebDAV, not a sync client.** Approaches considered: a pull/push CLI, a
  background two-way sync app, and WebDAV. A sync client has to know what
  changed (`nodes` has no change sequence) and what the person's _view_ lost
  when a grant is revoked on an ancestor. WebDAV sidesteps both, because nothing
  is copied. Its cost is that every file operation is a round trip, so rclone's
  VFS cache is the recommended client for agents.
- **Nothing about access is written a second time.** Every write is an
  `/api/v1` call answered in process by the agent router (`agent.go`,
  `innerRequest`) as the token's owner. So capability checks, team-folder
  rules (`authorizeMove`), editor locks, quota, the activity feed and the
  audit log are the ones the web client gets. Reads list through the service
  calls the listings use (`EntryPointsWithMeta`, `ListWithMeta`,
  `ListGrantedWithMeta`, `ExternalEntryPointsWithMeta`) and resolve
  capabilities in one batch (`share.AccessCaps`).
- **x/net/webdav for the XML, not for the semantics.** `PROPFIND`,
  `PROPPATCH`, `LOCK` and `UNLOCK` are answered by `golang.org/x/net/webdav`
  over adapters (`davFS`, `davFile`, `davInfo`, `davLocks`). `GET`, `HEAD`,
  `PUT`, `DELETE`, `MKCOL`, `COPY` and `MOVE` are answered here. The library
  maps every failure to fixed statuses: 405 for any PUT error, a delete before
  every overwrite. Neither fits 507, 423, a conflicted copy, or a version
  instead of a replacement.
- **Its own token.** A personal token with `agent_tokens.purpose = 'dav'`
  (migration `0025`), minted in Settings › Security › Mount as a drive. The
  lookup filters on purpose, so a WebDAV token presented to `/mcp/` (or the
  reverse) is not found. The instance switch is `webdav_enabled`
  (`system_config`, default **off**). The person's "allow agents" preference
  does not apply, because a mount is the person's own device.

## Authentication

`/dav` and `/dav/*` sit outside `/api/v1`, like `/mcp/`. They never read the
session cookie and are exempt from `sameOrigin`. `davAuth` runs on every
request:

1. `webdav_enabled` off → **404**.
2. The token comes from the Basic password (Finder and Explorer send nothing
   else) or from a Bearer header (rclone can send one). The user name is not
   consulted, because the token names its owner.
3. A missing, unknown, expired, revoked or wrong-purpose token gets
   **401** with `WWW-Authenticate: Basic realm="pp-hub"`. Refusals are limited
   per address (60/min, burst 20).
4. A suspended account gets **403**. Requests are limited per person
   (6000/min, burst 1200), because a mount stats every file it shows.
5. The audit marker is the token (`dav:<id>`), so reads and writes in the
   security log say which mount acted.

A browser that cached the Basic credential cannot be used across sites. Every
method except `GET` and `HEAD` needs a CORS preflight, which this endpoint never
answers, and `POST` is not served.

## Namespace

```
/                       the person's spaces (archived ones left out), and
                        "Shared with me" when other spaces shared something
/<space>/               the space's top level: their own items and the places shared with them
/<space>/<folder>/…     a folder, listed as the web client lists it
…/<Doc>.pages/          the sub-pages of <Doc>.md, beside the document
```

- Paths are **labels**. A space is shown under its name, with `/` and `\`
  replaced. A second entry with the same name in one listing becomes
  `Report (1a2b3c4d).docx` (the first 8 hex digits of its id), ordered by
  creation, so a label stays the same between listings.
- Names match in **Unicode NFC**: a path is normalized on arrival and
  compared to each entry's name in NFC, so a client that sends `Bùi`
  decomposed (Finder, other macOS apps) reaches the same entry the listing
  showed. New names are stored composed.
- Left out of every listing: extension nodes, the archive, the trash, and
  **files the person may view but not download**. Such a file cannot be read
  through a mount, and showing it would only produce errors.
- A pages folder is shown while the document has sub-pages, or for an hour
  after a client `MKCOL`s it. Only `.md` files can be created in it, and each
  becomes a sub-page (`POST /spaces/{id}/documents` with the document as
  `parent_id`, then its content through `PUT /nodes/{id}/text`). It cannot be
  deleted, moved or copied as a folder.
- `PROPFIND` with `Depth: infinity` is refused with **403**
  `propfind-finite-depth` (RFC 4918 §9.1). A missing `Depth` is read as 1.
- The space's top level carries `quota-used-bytes` and, when the space has a
  quota, `quota-available-bytes` (RFC 4331). `PROPPATCH` is accepted and
  dropped, because Finder and Explorer treat a refusal as a failed save.

## Reads

`GET` resolves the file and serves it through `GET /nodes/{id}/download`, with
`Range` and the conditional headers passed through. That route authorises it
(`CapDownload`) and audits it (deduplicated per hour). A block document open in
the editor is flushed first (`collab.Flush`), so the bytes are what its editors
see; so is one being copied or moved. The response carries
`ETag: "<blob hash>"`. `HEAD` answers from the version row and reads no bytes.

`PROPFIND` does not flush: a listing that wrote a version for every open
document each time Finder polled a folder would fill their histories. While
someone is typing in a document, its listed size and ETag can therefore trail
what the next `GET` returns, until the room's own write-back catches up.

The version handed over is **remembered per token** (`davState.reads`, 24 h,
bounded to 200 000 entries). It is the base the token's next write of that file
is checked against.

## Writes

| Method                                               | Becomes                                                                                                                                                                                                                                                                                                                                          |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `PUT`, new name in a folder or the space's top level | tus `POST /uploads` (creation-with-upload, the whole body in one request)                                                                                                                                                                                                                                                                        |
| `PUT`, new `.md` in a pages folder                   | `POST /spaces/{id}/documents` + `PUT /nodes/{id}/text`                                                                                                                                                                                                                                                                                           |
| `PUT` over a file                                    | tus upload naming `node_id`, which makes a new version                                                                                                                                                                                                                                                                                           |
| `PUT` over a block document                          | `PUT /nodes/{id}/text` with `base_etag`, so it goes through the room (`collab.Reseed`) and the people editing see it at once. Lossy markdown → **422**, larger than 1 MiB → **413**, not UTF-8 → **415**                                                                                                                                         |
| `MKCOL`                                              | `POST /spaces/{id}/dirs`                                                                                                                                                                                                                                                                                                                         |
| `DELETE`                                             | `DELETE /nodes/{id}`, which moves it to the **trash**                                                                                                                                                                                                                                                                                            |
| `MOVE`                                               | `PATCH /nodes/{id}/move`, then `PATCH /nodes/{id}/rename` in the destination (rename first when the destination already holds the old name; a failure part way is undone). A file moved to another space is copied there and the original trashed (the copy is removed if the original cannot be). A folder cannot move between spaces (**403**) |
| `COPY` of a file                                     | its bytes written at the destination (sub-pages are not copied; a client copying a folder copies the pages folder itself)                                                                                                                                                                                                                        |
| `COPY` of a folder                                   | `POST /nodes/{id}/copy`, then move and rename as needed; `Depth: 0` creates only the folder; not across spaces                                                                                                                                                                                                                                   |

- **Length.** `Content-Length`, or Finder's `X-Expected-Entity-Length` beside
  a chunked body. With neither, the body is spooled to a temporary file first
  (bounded by the upload cap), because the upload needs its length before the
  first byte. An upload that did not commit is terminated, so a failed write
  holds no quota.
- **Statuses.** A full space is **507** (the API's 413 "Quota Exceeded"),
  larger than the upload cap is **413**, someone else's WebDAV lock is **423**.
  Everything else is the API's own status and reason.
- **Names.** A new name that differs only in letter case from one already in
  the folder is refused with **409**, because macOS and Windows treat the two
  as one file. So is a name taken by something the caller is not shown (a
  file they may only view, someone's private item at a space's top level):
  the upload would otherwise land under a suffixed name the client never
  asked for.
- **Overwrite.** `MOVE`/`COPY` with `Overwrite: T` onto an existing node
  checks what it can first (a place to put it, a folder not crossing spaces),
  then trashes the destination and restores it if the operation still fails.
  A space, the root, "Shared with me" and a pages folder are never replaced
  (**403**), and neither are a source's own ancestors or descendants.

### Conflicts

A `PUT` (or the last step of a safe save) is checked against the version this
token last read. If someone has written since, the bytes land **beside** the
file as `Name (conflicted copy 2026-10-09 1432 <person>).ext` and the client
gets **201**. Nobody's work is overwritten. The same happens when an office
editor has the file open and the upload is refused before it opens, and when a
block document's room held unsaved edits that became a version during the
write (`PUT /text` answers 409). Outside "Shared with me" the copy lands in the
folder the client wrote to. In "Shared with me" it lands in the file's own
folder.

A write with no remembered base (the token never read the file, or the
server restarted) is last-writer-wins. An explicit `If-Match` replaces the
remembered base. `If-None-Match: *` on an existing file is **412**.

### Safe saves

Editors rarely write a file in place. Stored naively, each pattern below would
replace the file's node, and with it its version history, grants and comments.
Each instead becomes **a new version of the original node**:

| Pattern                                                                                                            | Who                           | Handling                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------------------------------------------------------------------------------------------------ | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Write a temporary copy, `MOVE` it over the file (`Overwrite: T`)                                                   | macOS safe save, many editors | The copy's bytes become the file's next version. The copy is purged if it was plainly the save's temporary (the caller's, one version, under 10 minutes old); otherwise it is trashed                                                                                                                                                                                                                                                      |
| Set the file aside under `~WRL0001.tmp` or `name~`, write the new copy under its name, `DELETE` the set-aside name | Word on Windows, vim, emacs   | The file is not renamed. The set-aside name becomes an alias that reads the version the file had (the editor's backup), and its own name looks empty until the new copy lands there as its next version. Deleting the alias before a save lands deletes the file. A set-aside whose save never lands within a minute is settled on the person's next request: a `~WRL` name is dropped (the file stays), a `name~` rename is done for real |
| `DELETE` the file, write the replacement under its name                                                            | some editors and scripts      | The trashed file is remembered for a minute. A write to its name restores it and makes the new bytes its next version. Folders are not remembered: a file written where a folder was is a new file                                                                                                                                                                                                                                         |

These are `davState.tombs` (a name set aside or trashed, kept for one minute)
and the set-aside aliases in `davState.scratch`. Setting a file aside takes
edit on it; reading an alias takes download on the file still — it is a name
for someone's file, not a copy — and is audited as a download of it.

### Scratch files

`.DS_Store`, `._*`, `Thumbs.db`, `desktop.ini`, `~$*` (Office owner files),
`.~lock.*#`, vim's swap files (`.*.swp` … `.swa`, `.swx`) and its `4913`
probe, `.goutputstream-*` and macOS `*.sb-<hex>-<id>` never become nodes. The
list is deliberately narrow: a name a person or an agent could mean —
`results.tmp`, `notes~`, Word's `~WRD0001.tmp` — is a file like any other,
because a scratch file reports success and is gone an hour later. They live
per person and per folder: bytes under `$TMPDIR/pp-dav-scratch/<process>`,
kept for an hour after their last write, writable only where the person may
create files, and bounded at 500 files and 1 GiB per person and 8 GiB per
process (**507** past that). A process reclaims what an earlier one left. A
real file with a scratch-like name (uploaded from the browser) stays a file,
and WebDAV writes to it are real writes. A folder cannot be created with a
scratch name, and a file cannot be moved to one in another folder.

### Locks

`LOCK` and `UNLOCK` are class 2, because Finder mounts read-only without them.
Locks are held **by a person against a node**, so two people reaching one file
by different paths contend for one lock. A name without a node is locked
against the caller's own path, and the lock moves to the node once the name
is written. A person's own locks never stand in their way: two machines of one
person are one person, and the version checks catch what they race. Taking a
lock needs **edit** on the file (or on the folder a new name is created in):
a view-only reader's lock would stop every editor's mount for a day. An
office editor's lock refuses a WebDAV lock on the file. A block document's
room does not, because writes go through it. Refusals are **423**; a person
holds at most 1000 locks.

WebDAV locks live in this process's memory (`davState.locks`) and last at most
a day. They are **not** `file_locks` rows. Finder and desktop Office hold a
lock for as long as a file is open, and a row would block the web editor with
nothing in the UI to show or break it. OnlyOffice and web uploads therefore
ignore WebDAV locks, and a mount's later save lands as a conflicted copy
instead. `LOCK` on an unmapped name reserves it (a lock-null resource, an empty
scratch entry) until the client writes it; `UNLOCK` before a write releases
the name.

### What is remembered, and what a restart costs

`davState` is in memory and disposable: remembered reads, tombs, scratch
files, pages folders and locks. A restart costs a conflict check its base (that
write becomes last-writer-wins), a safe save in flight its rename detection,
and clients their scratch files and locks. It never costs a byte of the
person's files. It assumes one server process, like the collaboration rooms.

## Clients

| Client                              | Notes                                                                                                                                                                                                                                                         |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **rclone** (recommended for agents) | `vendor = other`, user = email, pass = the token. Mount with `--vfs-cache-mode full` so tools that scan many files read from a local cache. macOS: `rclone nfsmount` (no macFUSE). Windows: `rclone mount` with WinFsp. rclone encodes names Windows forbids. |
| **Finder**                          | Go › Connect to Server › the URL. Works for light use. Writes go through `X-Expected-Entity-Length`.                                                                                                                                                          |
| **Windows Explorer**                | Map network drive. Needs HTTPS for Basic auth. The WebClient service caps files at ~50 MB by default (`FileSizeLimitInBytes`). Names with `\ : * ? " < > \|` or a trailing dot or space are not usable there.                                                 |

## Operations

- **Proxy.** The server answers `/dav` directly: no body limit, no
  buffering, no write timeout. A `PUT` is one request, so the limit that tus avoids for
  browser uploads (M30) applies here. A proxy placed in front must allow the
  same (no body limit, no buffering, long timeouts). The Vite dev proxy forwards `/dav` too, and its CORS middleware lets
  `OPTIONS` continue to the proxy (`preflightContinue`): otherwise Vite
  answers it itself, without the `DAV:` header and Basic challenge, and Finder
  cannot connect to `http://localhost:5173/dav/`.
- **Switch.** Admin › Settings › WebDAV, "Let people mount their drive over
  WebDAV" (`webdav_enabled`). Turning it off answers 404 at once. Tokens stay
  listed so they can be revoked.
- **Not covered.** Delta sync, offline work, files-on-demand placeholders,
  sharing over WebDAV, a change feed. The verification so far is real rclone
  commands against the production router (list, cat, copyto, mkdir, moveto,
  overwrite, delete) plus the database tests. Finder, Explorer, desktop Word
  and `rclone nfsmount`/`mount` have not been exercised.
