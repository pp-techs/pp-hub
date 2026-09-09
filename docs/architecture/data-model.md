# Data Model

All tables carry `tenant_id` (default `'default'`). Postgres is the only metadata store.

## Core Tables

```sql
spaces(id, tenant_id, name, owner_id, quota_bytes, used_bytes, created_at)

space_members(space_id, user_id, role)          -- owner | editor | viewer
  PRIMARY KEY (space_id, user_id)

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
       kind,                                    -- link | direct
       token_hash,                              -- sha256, link only; plaintext never stored
       grantee_user_id,                         -- direct only
       perm,                                    -- view | download | edit
       expires_at, created_by)

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
- **Effective permission**: (1) `space_members.role` for `node.space_id`; (2) nearest direct share on node/ancestor; (3) link token. MVP runs the 2-join lookup per request; no permission cache yet.

## Quota

`spaces.used_bytes` is maintained asynchronously by a quota job summing sizes of current versions per space. Uploads check against `quota_bytes` before committing a version.
