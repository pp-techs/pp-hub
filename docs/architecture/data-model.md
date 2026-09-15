# Data Model

All tables carry `tenant_id` (default `'default'`). Postgres is the only metadata store.

## Users Table

```sql
users(id, tenant_id, external_id, email, name,            -- OIDC-sourced fields
      display_name, avatar_url, bio,                      -- user-editable profile
      settings,                                            -- JSONB preferences
      is_admin, last_seen, created_at)
  -- email_hash (sha256) for search, unique per tenant
```

Profile fields (`display_name`, `avatar_url`, `bio`, `settings`, `is_admin`, `last_seen`) are added in migration 0006. The `is_admin` flag gates admin-only endpoints.

## Core Tables

```sql
spaces(id, tenant_id, name, owner_id, quota_bytes, used_bytes, created_at,
       personal_root_id)                        -- folder holding per-member personal dirs

space_members(space_id, user_id, role)          -- admin | member
  PRIMARY KEY (space_id, user_id)
  -- A space IS a company. `member` grants no file access at all; see
  -- permissions.md. Was owner|editor|viewer before migration 0011.

groups(id, tenant_id, space_id, name, description, is_everyone, created_by, created_at)
  UNIQUE (space_id, lower(name))
  UNIQUE (space_id) WHERE is_everyone           -- exactly one auto group per space

group_members(group_id, user_id, added_at)
  PRIMARY KEY (group_id, user_id)
  INDEX (user_id)                               -- resolution probes user-first

nodes(id, tenant_id, space_id, parent_id,       -- NULL parent = space root
      kind,                                     -- dir | file
      name,
      current_version_id,
      deleted_at,                               -- soft delete (trash)
      created_by, created_at)
  UNIQUE (parent_id, name) WHERE deleted_at IS NULL

node_versions(id, node_id, blob_hash, size, mime,
              version_no, created_by, created_at)  -- immutable, append-only

blobs(hash, size, ref_count, created_at)        -- blake3 hash = PK

file_locks(node_id, lock_token, expires_at)     -- WOPI locks

shares(id, tenant_id, node_id,                  -- file or folder
       kind,                                    -- link | direct | group
       token_hash,                              -- sha256, link only; plaintext never stored
       grantee_user_id,                         -- direct only
       grantee_group_id,                        -- group only
       perm,                                    -- view | download | edit
       expires_at, created_by)
  UNIQUE (node_id, grantee_group_id) WHERE kind = 'group'
  -- One grant per (folder, group): re-granting updates rather than stacking a
  -- second row that would then have to be resolved against the first.

previews(node_version_id, kind, blob_hash)      -- kind: thumbnail | pdf | video | text

uploads(id, node_target, temp_path, offset, expires_at)  -- tus resumable state

jobs(id, kind, payload, status, run_at, attempts)        -- preview, quota, GC
```

## Invariants

- `node_versions` are immutable; restore = flip `nodes.current_version_id`.
- Every `blob_hash` referenced by a version exists in `blobs`; `ref_count` tracks references (counter + periodic recount job — counter drift is a known, tolerated risk).
- A file node always has at least one version.
- CAS dedup: identical content across files/spaces shares one blob row.

## Query Patterns

- **List a folder**: `WHERE space_id AND parent_id AND deleted_at IS NULL` — single indexed query. No ltree/materialized path in MVP: move = update one row; recursive subtree listing is rare.
- **Version history**: `WHERE node_id ORDER BY version_no DESC`.
- **Search**: `nodes.name ILIKE '%…%'`, answered by the GIN trigram index `nodes_name_trgm` (migration `0007_search.sql`, partial on `deleted_at IS NULL`). A B-tree cannot serve an infix match, and people search for `forecast` expecting `Q1-forecast-final.xlsx`. The same index backs `similarity()` for ranking, and results are scoped by joining `space_members` — supported by `space_members_by_user`, since the table's `(space_id, user_id)` primary key cannot serve a user-first probe.
- **Trash**: `WHERE space_id AND deleted_at IS NOT NULL`. `ListDeletedWithMeta` joins the current version and owner, and adds two derived columns — `location` (ancestor names joined with `" / "`, from a recursive CTE) and `child_count` (direct live children) — so a row can say where it came from and how much comes back with it.
- **Effective permission**: `NodeAccessRanks` (`queries/shares.sql`) — one recursive CTE that takes an array of node ids and returns the highest rank the caller holds on each, via a direct share or a group grant on the node **or any ancestor**. Batched on purpose: a listing resolves every row in one query instead of one walk per row. It replaced a loop issuing one `GetNodeParent` per tree level per node, which also stopped widening at depth 100.

  The recursive join deliberately omits `deleted_at IS NULL`, unlike `GetNodeParent`. Filtering it truncates the chain at the first trashed ancestor and loses the grant above it — which is why trash used to bypass share resolution entirely. `ListDeletedWithMeta` walks the same way for the same reason.

## Trash Lifecycle

`Delete` tombstones exactly one row — `deleted_at = now()` on the node it was given, never its subtree. Descendants stay live but unreachable, because every listing query filters on the parent chain. Restoring the folder therefore brings the whole subtree back with one `UPDATE`, and no bulk operation is needed for either direction.

Two things can block a restore, and both are resolved rather than surfaced as errors:

- **The original parent is itself in the trash.** There is no folder to return to, so the node is re-parented to the top level of its space and the response sets `relocated: true`.
- **A live sibling has taken the name.** `nodes_name_uniq` is partial on `deleted_at IS NULL`, so the tombstoned row never conflicted while it sat in the trash — the collision only appears at the moment of restore. `fs.Restore` retries with `name (2)`, `name (3)`, … up to 50 and reports the original in `renamed_from`.

`RestoreNode` sets `deleted_at`, `parent_id` and `name` in one statement so neither adjustment needs a second write.

**Purge** is a hard `DELETE`. Descendants and versions follow through `ON DELETE CASCADE`; `shares` cascade from `node_id`. Before deleting, `ListSubtreeBlobHashes` collects one row per version in the subtree so `DecrBlobRef` can be called exactly as many times as `IncrBlobRef` was during upload. The delete and the decrements are not in one transaction — `fs.Service` holds a `*store.Queries`, not a pool — so they run in the order that fails safe: a crash between them leaves `ref_count` too **high**, which only delays garbage collection. The reverse order could drop a blob another node still points at.

`EmptyTrash` purges each tombstone in turn and tolerates `ErrNotFound`, since a nested tombstone often disappears with its ancestor's cascade before its own turn comes.

## Stars & Activity

`node_stars` is keyed `(user_id, node_id)`: a star is one person's bookmark, not a property of the node, and the composite primary key makes starring idempotent via `ON CONFLICT DO NOTHING`.

`activity_events` is the app's memory. Two deliberate choices:

- `node_id` is nullable and `ON DELETE SET NULL`, while `node_name` is denormalised onto the row. Purging a file therefore does not erase the record that it once existed and was deleted — the feed keeps its own copy of the name for exactly that case.
- `kind` is `TEXT`, not an enum, so adding a verb is a code change rather than a migration.

Recording is best-effort: an event describes work that already succeeded, so a failed insert is logged and swallowed rather than failing the user's operation.

## Quota

`spaces.used_bytes` is _intended_ to be maintained by a quota job summing the sizes of current versions per space, and `UpdateSpaceUsedBytes` exists for it — but nothing calls that query yet, and no upload checks `quota_bytes` before committing a version. Until a job is wired up, `used_bytes` stays at its default of `0`.

Both fields are returned by `GET /api/v1/spaces`, and the rail's storage meter renders the ratio between them — `quota_bytes = 0` means unlimited and the meter shows only the used figure with no bar. The meter is therefore correct in shape and empty in practice; it will start reading true the moment a recount job lands, with no change on the web side.
