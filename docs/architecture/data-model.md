# Data Model

All tables carry `tenant_id` (default `'default'`). Postgres is the only metadata store.

## Users Table

```sql
users(id, tenant_id, oidc_sub, oidc_iss, email, name,     -- OIDC-sourced fields
      display_name, avatar_url, bio,                      -- user-editable profile
      avatar_blob, avatar_mime,                            -- the CAS hash and mime an avatar_url resolves to
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
is the blake3 hash `handleUploadAvatar` resolved through `s.blobs.Put`, `avatar_mime` is the
type sniffed from the bytes (never the filename — it is served back with this value), and
`avatar_url` stays the rendered `/api/v1/users/{userID}/avatar?v=<hash prefix>` every DTO
already copies through, so the query suffix is what busts a browser's cache of the previous
picture. `GET /api/v1/users/{userID}/avatar` resolves `avatar_blob` back to bytes; re-uploading
identical bytes does not re-`IncrBlobRef` the same hash twice, mirroring `fs.PutFile`.

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

`migrations/0003_security_events.sql`. The audit trail for everything that changes who can
reach what: sign-ins and their failures, session revocations, admin and suspension changes,
grants, share revocation and membership changes. Written by `internal/audit`, read by
`GET /api/v1/admin/security-events`.

```sql
security_events(id,
                kind,                           -- TEXT verb, not an enum
                user_id,                        -- NULLABLE; ON DELETE SET NULL from users
                subject,                        -- free text: email, OIDC subject or error code
                ip, user_agent, request_id,     -- request provenance
                detail,                         -- JSONB, default '{}'
                created_at)
  INDEX (created_at DESC)                       -- security_events_recent: the console's tail
  INDEX (user_id, created_at DESC)              -- security_events_by_user, partial on NOT NULL
  INDEX (kind, created_at DESC)                 -- security_events_by_kind
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
  `DeleteSecurityEventsBefore` runs on the same five-minute ticker in `cmd/server` that purges
  expired sessions and sweeps WOPI locks, with the cutoff from
  `PP_AUDIT_RETENTION_DAYS` (365).
- **Writes are best-effort but not cancellable.** `audit.Record` swallows a failed insert with
  an **error**-level log rather than failing the operation it describes — the event already
  happened — but it writes on `context.WithoutCancel`, because a caller disconnecting
  mid-request is exactly when the row matters.
- `audit.Kind` is `TEXT`, so adding a verb is a code change rather than a migration. One kind,
  `share_created`, is defined and currently has no writer: creating a share records an activity
  row only, while revoking one records both.

## Core Tables

The development-phase migrations were consolidated into one baseline,
`migrations/0001_init.sql`, so the schema reads as a single document. It is the
authority; the sketches below are a map of it.

```sql
spaces(id, tenant_id, name, owner_id, quota_bytes, used_bytes,
       slug, description, archived_at, trash_retention_days,
       archive_root_id,                         -- the space's Archive folder
       default_as_admin,                        -- do new members land in Admin?
       allow_public_links,                      -- may anyone mint a /s/ link here?
       created_at)

space_members(space_id, user_id, created_at)
  PRIMARY KEY (space_id, user_id)
  INDEX (user_id)                               -- search/listing probes user-first
  -- No role column. A space IS a company; membership grants no file access at
  -- all, and admin status is membership in the space's Admin group. See
  -- permissions.md.

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

blobs(hash, size, ref_count, created_at)        -- blake3 hash = PK

file_locks(node_id, lock_token, expires_at)     -- WOPI locks

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
  CHECK (level <> 'edit' OR can_download)       -- an editor can fetch what it edits
  CHECK (kind <> 'link' OR (level = 'view' AND NOT can_share))
  -- One grant per (folder, group): re-granting updates rather than stacking a
  -- second row that would then have to be resolved against the first.
  -- Level + flags, not a rank: "editor who cannot re-share" and "viewer who
  -- may pass it on" are both expressible. Links never edit and never chain.

previews(node_version_id, kind, blob_hash)      -- kind: thumbnail | text (the artifact)

uploads(id, node_target, temp_path, offset, expires_at)  -- tus resumable state

jobs(id, kind, payload, status, run_at, attempts)        -- kind routes to a worker;
                                                         -- run_at = claimed-at while running
```

## Invariants

- `node_versions` are immutable; restore = flip `nodes.current_version_id`.
- Every `blob_hash` referenced by a version exists in `blobs`; `ref_count` tracks references (counter + periodic recount job — counter drift is a known, tolerated risk).
- A file node always has at least one version.
- Uploading into a folder never overwrites: `PutFile` with a zero `fileID` always creates a node, suffixing the name (`report (2).txt`) when it is taken. A new version of an existing file is an explicit `POST /nodes/{id}/versions`. Ownership must not depend on whether a filename happened to collide.
- CAS dedup: identical content across files/spaces shares one blob row.
- Every node has an owner: `nodes.created_by` is NOT NULL, and it is the only unconditional access path in the system. Ownership inherits down the subtree, so a node inside somebody else's folder is reachable by both creators.
- Every space has an `Everyone` group, an `Admin` group and an `archive_root_id`. `spaces.Service` provisions all three at creation; the uniqueness indices assume they exist.
- No node is its own parent (`nodes_no_self_parent_ck`), and every recursive ancestor/descendant walk stops at depth 100. `NodeAccessCaps` recurses with `UNION ALL`, so one cycle would hang capability resolution for everything beneath it rather than return a wrong answer.
- A team folder is top level (`nodes_team_root_top_level_ck`) and is its own container: `team_root_id = id`. Everything inside carries that id, inherited by `CreateNode` and rewritten across the subtree by `fs.Move` — the only two operations that can change containment. Inside one, `created_by` grants nothing; see the Team Folders section of [permissions.md](permissions.md).

## Query Patterns

- **List a folder**: `WHERE space_id AND parent_id AND deleted_at IS NULL` — single indexed query. No ltree/materialized path in MVP: move = update one row; recursive subtree listing is rare.
- **Version history**: `WHERE node_id ORDER BY version_no DESC`.
- **Search**: `nodes.name ILIKE '%…%'`, answered by the GIN trigram index `nodes_name_trgm` (partial on `deleted_at IS NULL`). A B-tree cannot serve an infix match, and people search for `forecast` expecting `Q1-forecast-final.xlsx`. The same index backs `similarity()` for ranking, and results are scoped by joining `space_members` — supported by `space_members_by_user`, since the table's `(space_id, user_id)` primary key cannot serve a user-first probe.
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

**Purge** is a hard `DELETE`, but not an indiscriminate one. `fs.Purge` first lifts out every topmost node in the subtree that the purged node's owner did not create (`ListForeignTopmostInSubtree` → `DetachNodeToTopLevel`), re-parenting each to `NULL` — its owner's top level — with a `" (n)"` suffix if the name is taken up there, and keeping `deleted_at` so a colleague's own tombstone stays in their trash. Only then does the `DELETE` run: descendants and versions follow through `ON DELETE CASCADE`, `shares` cascade from `node_id`, and the rescued rows are no longer in the subtree to be reached. Before deleting, `ListSubtreeBlobHashes` collects one row per version in the subtree — after the rescue, so surviving files keep their references — and `DecrBlobRef` is called exactly as many times as `IncrBlobRef` was during upload. The delete and the decrements are not in one transaction — `fs.Service` holds a `*store.Queries`, not a pool — so they run in the order that fails safe: a crash between them leaves `ref_count` too **high**, which only delays garbage collection. The reverse order could drop a blob another node still points at.

`EmptyTrash` purges each tombstone in turn and tolerates `ErrNotFound`, since a nested tombstone often disappears with its ancestor's cascade before its own turn comes.

## Stars & Activity

`node_stars` is keyed `(user_id, node_id)`: a star is one person's bookmark, not a property of the node, and the composite primary key makes starring idempotent via `ON CONFLICT DO NOTHING`.

`activity_events` is the app's memory. Two deliberate choices:

- `node_id` is nullable and `ON DELETE SET NULL`, while `node_name` is denormalised onto the row. Purging a file therefore does not erase the record that it once existed and was deleted — the feed keeps its own copy of the name for exactly that case.
- `kind` is `TEXT`, not an enum, so adding a verb is a code change rather than a migration.

Recording is best-effort: an event describes work that already succeeded, so a failed insert is logged and swallowed rather than failing the user's operation.

## Quota

`fs.checkQuota` rejects an upload that would push a space past `quota_bytes`, answering **413** with `fs.ErrQuotaExceeded`. It measures the delta a new version introduces, not the file's full size, so replacing a 10 MB file with a 11 MB one costs 1 MB. A space with `quota_bytes = 0` is unmetered.

`spaces.used_bytes` is recomputed by `fs.RecountUsage` — one `SUM` over the current versions of live files — after any mutation that can change the total (commit, delete, restore, purge, empty-trash). A recount is cheap at this scale and cannot drift, unlike an incremental counter.

Both fields are returned by `GET /api/v1/spaces`, and the rail's storage meter renders the ratio between them; `quota_bytes = 0` shows the used figure with no bar.
