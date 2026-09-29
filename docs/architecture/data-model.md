# Data Model

Postgres is the only metadata store.

**`tenant_id` is a dormant column, not a mechanism**, and the sentence that
used to stand here — "all tables carry `tenant_id`" — was never true. It sits
on the tables the baseline `0001_init.sql` created (`users`, `spaces`,
`groups`, `nodes`, `uploads`, `shares`, `jobs`, `activity_events`) as
`TEXT NOT NULL DEFAULT 'default'`, and on nothing added since: `sessions`
(`0002`), `security_events` (`0003`), `space_departures` (`0005`) and
`detached_orgs` (`0012`) have no
such column. No INSERT anywhere supplies a value, so the default is the only
value in the database. It is read by exactly three predicates in
`queries/users.sql` — `SearchUsers`, `ListUsers`, `CountUsers` —
whose only call sites pass the literal `"default"`
(`internal/httpapi/users.go:239,270,276`). There is no tenant middleware, no
request-scoped tenant, no `PP_TENANT*` variable and no row-level security.
Multi-tenancy is a project nobody has started; a space is the unit the product
actually models, and an organization links to that — see
[logto-org-sync.md](logto-org-sync.md).

## Users Table

```sql
users(id, tenant_id, oidc_sub, oidc_iss, email, name,     -- OIDC-sourced fields
      phone, gender,                                       -- IdP-owned, mirrored, read-only here
      display_name, avatar_url, bio,                      -- user-editable profile
      avatar_blob, avatar_mime,                            -- the CAS hash and mime an avatar_url resolves to
      idp_picture, avatar_from_idp,                        -- IdP `picture` last seen; whose the avatar is
      settings,                                            -- JSONB preferences
      is_admin, last_seen, suspended_at, created_at)
  UNIQUE (oidc_iss, oidc_sub)                              -- the identity key
  -- email_hash (sha256, set by a trigger) unique per tenant where non-empty
```

Profile fields (`display_name`, `avatar_url`, `bio`, `settings`, `is_admin`, `last_seen`) sit
alongside the OIDC-sourced ones in the baseline. The `is_admin` flag gates admin-only endpoints.
`last_seen` means last seen: `TouchSession` writes it in the same statement that slides a
session's deadline, so it moves with actual use. It used to be stamped only by the sign-in path,
which made the People table's "last active" column a login timestamp wearing another name.

An avatar's bytes live in the CAS like everything else uploaded (M22–M24 pass): `avatar_blob`
is the blake3 hash `users.Service.UploadAvatar` resolved through `blobs.Put`, `avatar_mime` is
the type sniffed from the bytes (never the filename — it is served back with this value), and
`avatar_url` stays the rendered `/api/v1/users/{userID}/avatar?v=<hash prefix>` every DTO
already copies through, so the query suffix is what busts a browser's cache of the previous
picture. `GET /api/v1/users/{userID}/avatar` resolves `avatar_blob` back to bytes. The column is
the reference: a trigger counts it into `blobs.ref_count` when it changes, so re-uploading the
same picture moves nothing. Every avatar write is conditional on the blob it read (`prev_blob`),
so of two concurrent writers exactly one lands and the other gets `ErrAvatarConflict`; the
loser's blob is left unreferenced for GC once the store's claim lapses.

### Profile fields the IdP owns

`users.Service.SyncIdPProfile` mirrors the IdP's `picture`, `phone_number` and `gender` at every
sign-in (from the ID token) and on every organization reconcile (from Logto's Management API
`avatar`, `primaryPhone`, `profile.gender` — the same values, so the two never undo each other).
It writes only what changed.

- **phone / gender** are replaced outright and are read-only in the app. A phone of bare digits
  (Logto's storage form) is given its E.164 `+`. `phone_number` needs the `phone` scope, which is
  requested only with `PP_OIDC_PHONE_SCOPE=1` — see [deployment.md](../deployment.md).
- **The avatar** is fetched into the CAS rather than hotlinked, so the CSP keeps `img-src 'self'`
  and an IdP serving avatars on an internal hostname still works. The most recent change on either
  side wins: a new `picture` replaces what is shown, an upload replaces the IdP's until its
  `picture` changes again. `idp_picture` is the URL last seen and is compared to decide whether to
  fetch; `NULL` means never observed, which is what lets an upload made before this existed survive
  its first sync. An IdP that drops its picture clears the avatar only when `avatar_from_idp`.
  Anything that does not sniff as PNG, JPEG, GIF or WebP — an SVG, say — is refused and
  remembered, so it is not refetched each sign-in. A network failure is not remembered and retries.
- **Nothing here refuses a sign-in.** A sync failure is logged and the login proceeds.

A user is identified by `(oidc_iss, oidc_sub)` — `GetUserByIdentity` — with `email_hash` unique
per tenant. Those two facts collide when an operator changes the issuer URL: every returning
user looks brand new, and the insert that follows hits the email uniqueness index. So a miss on
the identity falls back to the **verified** email and re-points the existing row at the new
`(iss, sub)` pair (`AdoptUserIdentity`) instead of creating a second account. The unique
constraint is unchanged; what changed is that a collision is now reached only by an identity
that has no right to the address, and is reported as such rather than as a 500.

## Sessions

`migrations/0002_sessions.sql`. A login is a row, not a signed cookie — the cookie it replaced
(`v1.<uuid>.<expiry>.<hmac>`) could not be revoked, listed or survive a key rotation.

```sql
sessions(id, user_id,                           -- ON DELETE CASCADE from users
         token_hash,                            -- sha256 of the cookie; UNIQUE
         user_agent, ip,                        -- what the Sessions tab shows
         created_at,                            -- the absolute deadline is measured from here
         last_used_at,                          -- slid lazily, at most hourly
         expires_at)                            -- the idle deadline
  INDEX (user_id, last_used_at DESC)            -- sessions_user_id: list and bulk-revoke
  INDEX (expires_at)                            -- sessions_expires_at: the purge sweep
```

- **Only the hash is stored.** The 256-bit token exists in the browser and nowhere else, for the
  same reason password hashes exist: a dump of this table yields nothing that can be presented
  as a cookie. Lookup is by hash, so the invariant costs nothing — it is still one indexed read.
- **Two deadlines, and both are needed.** `expires_at` is the idle deadline (30 days), pushed
  forward by `TouchSession` as the session is used; the absolute cap (90 days) is enforced in Go
  against `created_at`, because a deadline that traffic can extend indefinitely is not a
  timeout. `GetSessionByHash` filters `expires_at > now()` in SQL rather than in Go, so an
  expired row can never be touched back to life by the refresh that follows it.
- **The refresh is lazy.** `Authenticate` only writes when `last_used_at` is more than an hour
  old, so an active tab costs one `UPDATE` an hour rather than one per request, and a failed
  refresh is logged rather than failing a request that is otherwise valid.
- **Cascade, not cleanup.** `user_id` is `ON DELETE CASCADE`, so deleting a user cannot leave a
  live session behind. `DeleteExpiredSessions` is housekeeping only — expired rows are already
  refused on lookup — and runs on a five-minute ticker in `cmd/server`.
- **Revocation is scoped by owner.** `DeleteUserSession` takes `(id, user_id)` and
  `DeleteOtherUserSessions` takes `(user_id, id_to_keep)`, so guessing another person's session
  id achieves nothing and "sign out everywhere else" keeps exactly the session that asked.

## Security Events

`migrations/0003_security_events.sql`, plus `0008_read_audit.sql` for `node_id`. The audit
trail for everything that changes who can reach what — sign-ins and their failures, session
revocations, admin and suspension changes, grants, shares and membership changes — and, since
M26, who read which file and the alerts raised on the log itself. Written by `internal/audit`
(and `internal/alert`), read by `GET /api/v1/admin/security-events` and its `/export`.

```sql
security_events(id,
                kind,                           -- TEXT verb, not an enum
                user_id,                        -- NULLABLE; ON DELETE SET NULL from users
                subject,                        -- free text: email, OIDC subject or error code
                ip, user_agent, request_id,     -- request provenance
                detail,                         -- JSONB, default '{}'
                created_at,
                node_id)                        -- NULLABLE; deliberately no FK to nodes
  INDEX (created_at DESC)                       -- security_events_recent: the console's tail
  INDEX (user_id, created_at DESC)              -- security_events_by_user, partial on NOT NULL
  INDEX (kind, created_at DESC)                 -- security_events_by_kind
  INDEX (node_id, created_at DESC)              -- security_events_by_node, partial on NOT NULL
```

**Why this is a second table and not more rows in `activity_events`.** Two columns on that
table are `NOT NULL` and both are wrong here: `space_id`, because an authentication event
belongs to no space, and `actor_id`, because the _interesting_ authentication event is the
failed one — where there may be no local account to point at, and pointing at one would be a
claim the server cannot make. The two tables are also read by different people for different
reasons: `activity_events` is a feed of work for colleagues, this is a record of access for
whoever has to answer "who got in, from where, and when did that permission appear". Nothing
about the feed's schema, scoping or readers wanted to become an audit log.

- **The three indexes answer the three questions actually asked.** `created_at DESC` is the
  console's default page — the tail, newest first. `(user_id, created_at DESC)` is "everything
  this account did", and it is partial on `user_id IS NOT NULL` because the rows with no user
  are exactly the ones that question never reaches. `(kind, created_at DESC)` is "every failed
  sign-in", which is the `?kind=` filter and the one query an incident starts from.
- **`user_id` is nullable and `ON DELETE SET NULL`, not `CASCADE`.** Null covers two cases: a
  sign-in that failed before an identity was resolved, and an account deleted since. Cascading
  would mean deleting a user erases the record that they signed in, which is the whole point of
  keeping one — the same reasoning that makes `activity_events.node_id` a `SET NULL` with the
  name denormalised beside it.
- **`subject` is free text, and what it carries depends on the event.** On a successful
  sign-in, a logout or an admin change it is the account's email — a durable label that still
  reads as a person after `user_id` has been nulled. On a failed sign-in it is the failure
  _code_ rather than an address (`state`, `token`, `exchange`, `email-taken`, `suspended`,
  `internal`), because `failLogin` records attempts that never resolved an identity and a code
  is what there is to say. On `identity_adopted` it is the verified email the adoption matched
  on. The console joins `users.email` for a live account and falls back to this column when
  the join misses.
- **`detail` is JSONB for the per-kind extras** — the revoked session id, `{"revoked": n}` on a
  bulk revocation, the old and new issuer on an adoption, `{"via":
"PP_BOOTSTRAP_ADMIN_EMAIL"}` on a bootstrap promotion. An empty object is omitted from the
  wire, so the API shape stays honest about which events carry extras.
- **`user_agent` is truncated to 400 bytes** before it reaches the column. It is
  client-controlled text and there is no reason to accept a megabyte of it.
- **Retention is real.** One row is written on every sign-in and every failed attempt, so
  "forever" is a slow leak and eventually a console nobody can read.
  `DeleteSecurityEventsBefore` and `DeleteReadEventsBefore` run on the same five-minute ticker
  in `cmd/server` that purges expired sessions and sweeps WOPI locks: read kinds against
  `read_audit_retention_days` (90), everything else against `audit_retention_days`
  (`PP_AUDIT_RETENTION_DAYS`, 365).
- **`node_id` has no foreign key on purpose.** A purged file must not erase or null out the
  record that somebody read it — the `user_id ... SET NULL` reasoning taken one step further,
  because here the id is the only thing left to filter on. The migration backfilled it from the
  `detail.node` grant and share events already carried. The console's `?node=` filter walks the
  subtree (trashed descendants included) and also matches the id directly, so a purged node
  still answers; the name comes from `nodes` while the node exists, else from `detail.name`.
- **Reads are deduplicated before they reach the table.** `file_downloaded`,
  `folder_downloaded`, `file_viewed` and `file_opened` go through `audit.RecordRead`, which keeps
  one row per (kind, actor, node) per hour in a bounded in-memory map — per replica, so N
  replicas can write N rows per window. A share-link visitor has no user; its actor is the link
  plus the client address, and its `subject` is `share_link`.
- **Writes are best-effort but not cancellable.** `audit.Record` swallows a failed insert with
  an **error**-level log rather than failing the operation it describes — the event already
  happened — but it writes on `context.WithoutCancel`, because a caller disconnecting
  mid-request is exactly when the row matters.
- `audit.Kind` is `TEXT`, so adding a verb is a code change rather than a migration.
  `security_alert` rows have no request behind them: `internal/alert` writes them through
  `RecordSystemSecurityEvent` with the offending address in `ip` and the rule in `subject`.

## System Configuration

`migrations/0006_system_config.sql`. Key-value JSONB store for dynamic operational and policy knobs (e.g. `instance_name`, `max_upload_bytes`, `audit_retention_days`, `default_space_quota_bytes`, and since M30 `version_retention_count`).

```sql
system_config(key, tenant_id, value, updated_at, updated_by)
```

- **Hierarchy**: Setting in `system_config` takes precedence over environment variable fallbacks (`PP_MAX_UPLOAD_BYTES`, `PP_AUDIT_RETENTION_DAYS`) and built-in defaults.
- **Audit trail**: Every update or reset via `PATCH /api/v1/admin/settings` or `DELETE /api/v1/admin/settings/{key}` writes a `settings_updated` event to `security_events` with the operator's `user_id` and modified keys.
- **In-memory cache**: `internal/sysconfig.Service` caches settings in-memory to ensure zero-latency checks on the upload path and during background sweeps, refreshed on write and periodically on the 5-minute background sweep across replicas.

## Core Tables

The development-phase migrations were consolidated into one baseline,
`migrations/0001_init.sql`, so the schema reads as a single document. It is the
authority, together with the migrations that follow it; the sketches below are
a map of both.

```sql
spaces(id, tenant_id, name, owner_id, quota_bytes, used_bytes,
       slug, description, archived_at, trash_retention_days,
       archive_root_id,                         -- the space's Archive folder
       default_as_admin,                        -- do new members land in Admin?
       allow_public_links,                      -- may anyone mint a /s/ link here?
       idp_org_id,                              -- the Logto organization this
                                                -- space mirrors; NULL = unlinked
       created_at)
  UNIQUE (idp_org_id) WHERE idp_org_id IS NOT NULL   -- spaces_idp_org_uniq
  -- Partial on purpose. Any number of spaces may sit unlinked — one that
  -- predates the sync, or one whose organization was deleted in Logto (a 404
  -- unlinks rather than empties it); reconciling it is a no-op until an
  -- instance admin links it (Link to Logto) — but a linked organization
  -- belongs to exactly one space: two spaces mirroring one organization would
  -- each revoke the other's members on every reconcile. A plain UNIQUE would
  -- instead allow one unlinked space and refuse the second.
  -- spaces.name mirrors the organization's name: a rename writes it to Logto
  -- first, and every reconcile copies the organization's name back.

space_members(space_id, user_id, created_at)
  PRIMARY KEY (space_id, user_id)
  INDEX (user_id)                               -- search/listing probes user-first
  -- No role column. A space IS a company; membership grants no file access at
  -- all, and admin status is membership in the space's Admin group. See
  -- permissions.md.
  -- The table is a PROJECTION of spaces.idp_org_id's member list, written only
  -- by the orgsync reconciler. It stays a table rather than a call upstream
  -- because ten SQL sites join it to scope a query and thirteen Go guards
  -- read it; per-request consultation of an external service would rewrite
  -- every one of them. See logto-org-sync.md.

space_departures(space_id, user_id,             -- both ON DELETE CASCADE
                 detected_at,                   -- when a sync path first missed them
                 source,                        -- signin | webhook | reconcile | admin
                 resolved_at, resolved_by)      -- who confirmed the teardown, and when
  PRIMARY KEY (space_id, user_id)
  INDEX (space_id) WHERE resolved_at IS NULL    -- space_departures_pending
  -- The queue between the two halves of a departure. Losing the space_members
  -- row is immediate and automatic; the destructive half — grant teardown,
  -- the ownership climb, the move into Archive — is irreversible and waits for
  -- a space admin, so an accidental click in the Logto Console cannot trigger
  -- it. `source` is TEXT, not an enum, for the same reason audit.Kind is.
  -- Re-adding somebody upstream before the confirmation simply re-creates the
  -- membership; the row is what the People page reads, and the only hot query
  -- against it is "what is still pending in this space" — hence the partial
  -- index and no other.

detached_orgs(org_id,                           -- PK; a Logto organization id
              detached_at,
              detached_by)                      -- ON DELETE SET NULL from users
  -- (0012) Organizations whose space was deliberately deleted here. Deleting a
  -- space does not delete its organization, but its members still carry it in
  -- their ID tokens and the next sign-in would create a fresh, empty space for
  -- it. The space delete inserts this row in the same transaction; sign-in and
  -- reconcile skip a detached organization (ensureSpace), and linking a space
  -- to it again deletes the row (ReattachOrg).

groups(id, tenant_id, space_id, name, description,
       is_everyone, is_admin,                   -- the two automatic groups
       created_by, created_at)
  UNIQUE (space_id, lower(name))
  UNIQUE (space_id) WHERE is_everyone           -- groups_everyone_uniq
  UNIQUE (space_id) WHERE is_admin              -- groups_admin_uniq

group_members(group_id, user_id, added_at)
  PRIMARY KEY (group_id, user_id)
  INDEX (user_id)                               -- resolution probes user-first

nodes(id, tenant_id, space_id, parent_id,       -- NULL parent = space root
      kind,                                     -- dir | file
      name,
      current_version_id,
      deleted_at,                               -- soft delete (trash)
      created_by,                               -- THE OWNER: full access here
                                                -- and everywhere beneath, no
                                                -- grant row involved — EXCEPT
                                                -- inside a team folder, where
                                                -- it is only a byline
      team_root_id,                             -- NULL = personal; own id on a
                                                -- team folder; the container's
                                                -- id for its contents
      created_at)
  UNIQUE (parent_id, name) WHERE deleted_at IS NULL

node_versions(id, node_id, blob_hash, size, mime,
              version_no, created_by, created_at)  -- immutable, append-only

blobs(hash, size, ref_count, created_at,       -- blake3 hash = PK
      released_at, pinned_until,               -- GC: when ref_count last hit 0; a write's claim (0011)
      verified_at, damaged_at)                 -- scrub bookkeeping (0011)
  -- ref_count is maintained by triggers on node_versions, previews,
  -- users.avatar_blob and node_attachments, cascades included; no application
  -- code moves it.

file_locks(node_id, lock_token, expires_at)     -- WOPI locks, and block-document rooms'
  -- A room's token is `collab:<nodeId>.<epoch>` (M39): taken when the room loads,
  -- refreshed every 60 s, released when it unloads; every `collab:` row is
  -- cleared at startup, since only this process's rooms can hold one.

node_attachments(node_id, blob_hash, name, mime,  -- images/files dropped into a block
                 size, created_by, created_at)    -- document (0013); not nodes, never listed
  PRIMARY KEY (node_id, blob_hash)               -- the markdown says `attachment:<hash>`
  -- node_id ON DELETE CASCADE; blob_hash REFERENCES blobs. Owned by the
  -- document, not a version: older versions may still name it. A copy of the
  -- document copies its rows, so no markdown needs rewriting.

shares(id, tenant_id, node_id,                  -- file or folder; inherits down
       kind,                                    -- link | direct | group
       token_hash,                              -- sha256, link only; plaintext never stored
       grantee_user_id,                         -- direct only
       grantee_group_id,                        -- group only
       level,                                   -- view | edit
       can_download,                            -- raw bytes
       can_share,                               -- may re-grant on this subtree
       expires_at, created_by, created_at)
  UNIQUE (node_id, grantee_group_id) WHERE kind = 'group'
  UNIQUE (node_id, grantee_user_id)  WHERE kind = 'direct'   -- 0009
  CHECK (level <> 'edit' OR can_download)       -- an editor can fetch what it edits
  CHECK (kind <> 'link' OR (level = 'view' AND NOT can_share))
  -- One grant per (node, group) and per (node, person): re-granting updates rather than stacking a
  -- second row that would then have to be resolved against the first.
  -- Level + flags, not a rank: "editor who cannot re-share" and "viewer who
  -- may pass it on" are both expressible. Links never edit and never chain.

previews(node_version_id, kind, blob_hash)      -- kind: thumbnail | text (the artifact)

uploads(id, tenant_id, space_id, parent_id, node_id, attach_to,  -- tus resumable state (0010)
        name, total_size, uploaded, temp_path, expires_at, completed_at,
        attachment_hash, created_by, created_at)
  CHECK (0 <= uploaded <= total_size)
  CHECK uploads_one_target (node_id IS NULL OR attach_to IS NULL)   -- 0013
  -- node_id set = the bytes become a new version of that file (ON DELETE CASCADE);
  -- attach_to set = an attachment of that block document (ON DELETE CASCADE), and
  -- attachment_hash records what it became, for a HEAD after a lost response;
  -- otherwise a new file `name` under parent_id. total_size is NOT NULL: the cap and
  -- the quota are checked against it at creation. temp_path is relative to
  -- <PP_BLOB_ROOT>/.uploads. expires_at slides 24h forward on every chunk;
  -- completed_at marks a committed upload, kept until expiry so HEAD can answer.

jobs(id, kind, payload, status, run_at, attempts)        -- kind routes to a worker;
                                                         -- run_at = claimed-at while running

node_texts(node_id PK, node_version_id, content, folded,  -- M27 content search (0016)
           tsv GENERATED (to_tsvector('simple', folded)), extracted_at)
  -- One row per file: the text of its *current* version, written by the
  -- extract:text job. content is NFC-normalised, capped, valid UTF-8 (what
  -- snippets are cut from); folded is content lower-cased without diacritics,
  -- rune for rune (textsearch.Fold), so a hit's position in folded is its
  -- position in content. GIN on tsv, GIN trigram on folded. Both FKs cascade.
```

## Comments and Notifications

Migration `0015_comments_notifications.sql` (M29):

```sql
comment_threads(id, node_id, created_by, created_at, resolved_at, resolved_by)
  -- node_id ON DELETE CASCADE: purged with the document; a trashed one keeps them.
  CHECK ((resolved_at IS NULL) = (resolved_by IS NULL))

comments(id, thread_id, author_id, body, created_at, edited_at)
  -- thread_id ON DELETE CASCADE; body <> ''. The service deletes a thread with
  -- its last comment, in the same transaction.

notifications(id, user_id, kind, actor_id, node_id, thread_id, comment_id,
              version_id, excerpt, created_at, read_at)
  -- kind IN ('comment', 'reply', 'mention'). node/thread/comment cascade, so a
  -- notification goes with what it points at. version_id is set by the
  -- notify:mentions job and is deliberately not a foreign key — pruning the
  -- version must not take the notification with it.
  UNIQUE (user_id, version_id) WHERE version_id IS NOT NULL  -- a retried job cannot notify twice
```

- A thread's anchor is not in the database: the markdown carries its id in a
  comment marker ([block-documents.md](block-documents.md#comment-anchors)).
  A thread whose marker was deleted from the text still exists and is listed.
- Read notifications are purged 30 days after they were read
  (`notifications.ReadRetention`) by the five-minute sweep in `cmd/server`;
  unread ones stay until read or until what they point at is deleted.
- Indexes serve the list (`user_id, created_at DESC, id DESC`, keyset paged),
  the unread count (partial on `read_at IS NULL`) and the purge (partial on
  `read_at IS NOT NULL`).

## Invariants

- `node_versions` are immutable; restore = flip `nodes.current_version_id`.
- Every `blob_hash` referenced by a version exists in `blobs` (the store claims the row before installing the bytes, and the version trigger creates it if somehow absent). `ref_count` equals the number of version, preview, avatar and attachment rows naming the hash — kept by trigger in the same statement as the reference, including the `node_attachments` rows a purge removes by cascade; the daily storage audit recounts and logs any correction as a bug. See [storage.md](storage.md#garbage-collection).
- An attachment lives exactly as long as its document: one row per (document, blob), attaching the same bytes twice is the row already there, and the reference is released only when the document is purged (a copy holds its own row).
- A `file_locks` row whose token starts `collab:` belongs to a block-document room. REST replacement is refused through it (423) like an OnlyOffice lock; restoring a version is let through, because the room reseeds itself.
- A file node always has at least one version.
- Uploading into a folder never overwrites: `PutFile` with a zero `fileID` always creates a node, suffixing the name (`report (2).txt`) when it is taken. A new version of an existing file is an explicit act — `POST /nodes/{id}/versions` until M30, a tus upload naming `node_id` since. Ownership must not depend on whether a filename happened to collide.
- `uploads.uploaded` never claims a byte the staging file does not durably hold: `uploads.Service.Append` fsyncs before advancing the row, and a staging file found shorter than the row (a database restored without `.uploads`) discards the upload rather than resuming into zeros.
- CAS dedup: identical content across files/spaces shares one blob row.
- Every node has an owner: `nodes.created_by` is NOT NULL, and it is the only unconditional access path in the system. Ownership inherits down the subtree, so a node inside somebody else's folder is reachable by both creators.
- Every space has an `Everyone` group, an `Admin` group and an `archive_root_id`. `spaces.Service` provisions all three at creation; the uniqueness indices assume they exist.
- `space_members` has exactly one writer, the `orgsync` reconciler, and a row exists if and only if the person is in the space's Logto organization. Nothing else inserts or deletes one, so "who is in this space" has a single answer and a single place it comes from.
- A pending `space_departures` row (`resolved_at IS NULL`) implies there is no `space_members` row for that pair: access is revoked first, and the row is the record that the teardown has not happened yet. The reverse does not hold — re-adding the person upstream re-creates the membership while the departure stays pending, because the only thing that resolves one is an admin's confirmation (`POST /spaces/{id}/departures/{userID}/complete`).
- No node is its own parent (`nodes_no_self_parent_ck`), and every recursive ancestor/descendant walk stops at depth 100. `NodeAccessCaps` recurses with `UNION ALL`, so one cycle would hang capability resolution for everything beneath it rather than return a wrong answer.
- A team folder is top level (`nodes_team_root_top_level_ck`) and is its own container: `team_root_id = id`. Everything inside carries that id, inherited by `CreateNode` and rewritten across the subtree by `fs.Move` — the only two operations that can change containment. Inside one, `created_by` grants nothing; see the Team Folders section of [permissions.md](permissions.md).

## Query Patterns

- **List a folder**: `WHERE space_id AND parent_id AND deleted_at IS NULL` — single indexed query. No ltree/materialized path in MVP: move = update one row; recursive subtree listing is rare.
- **Version history**: `WHERE node_id ORDER BY version_no DESC`.
- **Search**: `nodes.name ILIKE '%…%'`, answered by the GIN trigram index `nodes_name_trgm` (partial on `deleted_at IS NULL`). A B-tree cannot serve an infix match, and people search for `forecast` expecting `Q1-forecast-final.xlsx`. The same index backs `similarity()` for ranking, and results are scoped by joining `space_members` — supported by `space_members_by_user`, since the table's `(space_id, user_id)` primary key cannot serve a user-first probe. Since M27, `SearchNodes` also matches `node_texts` — the folded full-text query, or a folded substring as fallback — joined on `node_version_id = nodes.current_version_id` so stale text is never matched; every name hit ranks above every content-only hit, and `ts_rank` orders the content hits.
- **Trash**: `WHERE space_id AND deleted_at IS NOT NULL`. `ListDeletedWithMeta` joins the current version and owner, and adds two derived columns — `location` (ancestor names joined with `" / "`, from a recursive CTE) and `child_count` (direct live children) — so a row can say where it came from and how much comes back with it.
- **Effective capabilities**: `NodeAccessCaps` (`queries/shares.sql`) — one recursive CTE that takes an array of node ids and returns, per node, whether the caller owns it (`nodes.created_by` anywhere up the chain) plus the `BOOL_OR` of every grant flag reachable on the node **or any ancestor**, via a direct share or a group they belong to. Batched on purpose: a listing resolves every row in one query instead of one walk per row. It replaced a loop issuing one `GetNodeParent` per tree level per node, which also stopped widening at depth 100.
- **Buried content**: `ListTrashRootsWithUserContent` climbs from the caller's own live nodes to the first tombstoned ancestor, so the trash can show a folder somebody else trashed with the caller's work inside it. `CountForeignOwnedInSubtree` and `CountOwnedInSubtree` walk the other way for the "this holds N items other people made" warnings. All four subtree walks cap at depth 100.
- **Who reaches a node**: `ListChainPrincipals` returns the creator of the node and of every ancestor plus every live grant on that chain, as `(kind, principal_id, name)` rows. `fs.MoveImpact` differences three of those sets — node, current parent, destination — to answer "who gains and who loses access" before a move.
- **Entry points**: `ListEntryPoints` returns the topmost nodes a caller can reach — owned (`include_owned`) or granted — dropping any row that sits beneath another reachable one. It backs both a member's top level and "Shared with me", which is the same query with `include_owned = false`.

  The recursive join deliberately omits `deleted_at IS NULL`, unlike `GetNodeParent`. Filtering it truncates the chain at the first trashed ancestor and loses the grant above it — which is why trash used to bypass share resolution entirely. `ListDeletedWithMeta` walks the same way for the same reason.

## Trash Lifecycle

`Delete` tombstones exactly one row — `deleted_at = now()` on the node it was given, never its subtree. Descendants stay live but unreachable, because every listing query filters on the parent chain. Restoring the folder therefore brings the whole subtree back with one `UPDATE`, and no bulk operation is needed for either direction.

Two things can block a restore, and both are resolved rather than surfaced as errors:

- **The original parent is itself in the trash.** There is no folder to return to, so the node is re-parented to the top level of its space and the response sets `relocated: true`.
- **A live sibling has taken the name.** `nodes_name_uniq` is partial on `deleted_at IS NULL`, so the tombstoned row never conflicted while it sat in the trash — the collision only appears at the moment of restore. `fs.Restore` retries with `name (2)`, `name (3)`, … up to 50 and reports the original in `renamed_from`.

`RestoreNode` sets `deleted_at`, `parent_id` and `name` in one statement so neither adjustment needs a second write.

**Purge** is a hard `DELETE`, but not an indiscriminate one. `fs.Purge` first lifts out every topmost node in the subtree that the purged node's owner did not create (`ListForeignTopmostInSubtree` → `DetachNodeToTopLevel`), re-parenting each to `NULL` — its owner's top level — with a `" (n)"` suffix if the name is taken up there, and keeping `deleted_at` so a colleague's own tombstone stays in their trash. Only then does the `DELETE` run: descendants and versions follow through `ON DELETE CASCADE`, `shares` cascade from `node_id`, and the rescued rows are no longer in the subtree to be reached. The rescue and the delete are one transaction. Every version and preview the cascade removes releases its blob through the `ref_count` triggers in the same statement, so there is no separate decrement to order or to lose; rescued files keep their references because their rows are never deleted.

`EmptyTrash` purges each tombstone in turn and tolerates `ErrNotFound`, since a nested tombstone often disappears with its ancestor's cascade before its own turn comes.

## Stars & Activity

`node_stars` is keyed `(user_id, node_id)`: a star is one person's bookmark, not a property of the node, and the composite primary key makes starring idempotent via `ON CONFLICT DO NOTHING`.

`activity_events` is the app's memory. Two deliberate choices:

- `node_id` is nullable and `ON DELETE SET NULL`, while `node_name` is denormalised onto the row. Purging a file therefore does not erase the record that it once existed and was deleted — the feed keeps its own copy of the name for exactly that case.
- `kind` is `TEXT`, not an enum, so adding a verb is a code change rather than a migration.

Recording is best-effort: an event describes work that already succeeded, so a failed insert is logged and swallowed rather than failing the user's operation.

## Quota

`fs.EnsureRoom` rejects adding bytes that would push a space past `quota_bytes`, answering **413** with `fs.ErrQuotaExceeded`. Since M30 it compares `used + incoming` with no discount: a new version costs its full size, because the version it supersedes is still on disk. (Before M30, `fs.checkQuota` measured only the delta a new version introduced, so replacing a 10 MB file with an 11 MB one cost 1 MB — and every superseded version was invisible to the meter.) It runs twice for a tus upload: at creation against the declared length plus `PendingUploadBytes` — what other uploads still in flight have promised to the space, so parallel uploads cannot each pass a check they would fail together — and again in `fs.putFile` against the measured size once the bytes have landed, which is the enforcement; the declared figure only refuses early. The second check runs before the node is created, so a refused upload leaves no empty file behind. A space with `quota_bytes = 0` is unmetered.

`spaces.used_bytes` is recomputed by `fs.RecountUsage` — one `SUM` (`SumSpaceUsage`) over **every stored version of every file in the space, trash included**, plus every block-document attachment (M39) — after any mutation that can change the total. Until M30 it summed only the current versions of live files, which let the meter and the disk drift apart; migration `0010` recomputed every space under the new definition. `fs.PutFile` and `fs.PutStagedFile` recount themselves, so WOPI saves move the meter too. It sums version sizes, not distinct blobs, so a copy sharing its blob is counted for each version that references it. A recount is cheap at this scale and cannot drift, unlike an incremental counter.

**Version retention** keeps history bounded. `version_retention_count` (`system_config`, default 100, `0` keeps every version, at most 10000) is read on every save; after each new version `fs.pruneVersions` runs `PruneVersions`, deleting every version beyond the newest N — the current version is never a candidate and counts as one of the N. Previews cascade, and the triggers release both kinds of blob so GC reclaims them. It applies on each file's next save, not in a retroactive sweep.

`GET /api/v1/usage` (`SumSpaceUsageByCategory`) splits the same total: `trash` (every version of trashed files; `files` counts files), `versions` (superseded versions of live files; `files` counts versions), then current versions and a live document's attachments by MIME family — so the slices sum to `used_bytes`. An attachment is checked against the quota like a version: at upload creation against the declared length, and in `fs.AddStagedAttachment` against the measured size.

Both fields are returned by `GET /api/v1/spaces`, and the rail's storage meter renders the ratio between them; `quota_bytes = 0` shows the used figure with no bar.
