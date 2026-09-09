# Permissions

## Model

Two layers, deliberately minimal:

1. **Space roles** — every node inherits from its space: `viewer` (read/download), `editor` (read/write/upload/edit), `owner` (editor + member management + space settings).
2. **Per-node shares** — `direct` (to a specific user) or `link` (anonymous token). Permission values: `view` | `download` | `edit`.

A full per-node ACL system was rejected: query complexity and cache invalidation cost are too high for the MVP value.

## Resolution Order

Effective permission for a user on a node:

1. `space_members.role` for `node.space_id` → base permission.
2. Direct share on the node or its nearest ancestor → raises the effective permission. **Shares only widen, never narrow.**
3. Link share token (no authenticated user) → link permission, restricted to read paths (`view`/`download`). Edit-via-link is post-MVP.

## Implementation Notes

- Resolution is a 2-join lookup per request; no permission cache in MVP. If profiling shows it hot, add a request-scoped cache first, then an invalidated cache keyed by `(user, node)`.
- Share tokens: only `token_hash` (sha256) is stored; the plaintext is displayed once at creation. Revocation = row delete. Optional `expires_at`.
- WOPI access tokens encode the resolved permission at session-open time; changing space role mid-session does not retroactively change an open editor session (acceptable; tokens expire).

## Quotas

Per-space `quota_bytes`; enforced at version-commit time. Usage is maintained asynchronously (`used_bytes` by quota job), so a brief overshoot is possible and tolerated.
