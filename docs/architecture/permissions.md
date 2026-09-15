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

## Rank Mapping

`requireNodeAccess(minRank)` compares an _effective rank_ against the rank a route asks for, on the share scale: `view=1`, `download=2`, `edit=3`. Space roles map **onto that same scale**, not onto a parallel role scale:

| Space role | Effective rank |
| :--------- | :------------- |
| viewer     | 1 (view)       |
| editor     | 3 (edit)       |
| owner      | 3 (edit)       |

Editor and owner are identical here on purpose. Owner-only powers — member management, space settings, permanent deletion, emptying the trash — are gated by `RequireRole(RoleOwner)` at the space level and never travel through this scale.

This was previously a positional mapping (viewer=1, editor=2, owner=3), which meant an editor's rank was 2 while every mutating route asks for 3. The effect was that editors could upload — `handleUpload` checks the space role directly — but could not rename, move or delete what they had just uploaded. The role reference in the settings screen states the intent plainly: _"Editor — upload, edit, move, delete, share files, restore versions."_

Direct shares still only ever widen: a share grants rank to someone whose space role gives them less, never less than their role already gives.

## Suspension

`users.suspended_at` blocks a user without deleting their files or their authorship of anything. It is enforced in `requireUser`, on every request, rather than at sign-in — so suspending takes effect immediately instead of waiting for an open session to expire. The response is **403, not 401**: the credentials are valid, the account is not, and a 401 would send the client into a pointless re-authentication loop.

An admin cannot revoke their own admin flag or suspend themselves. Both are refused server-side, because either would risk leaving the install unadministrable with no route back through the UI.

## Invitations

An invitation names an email address; membership names an account. That gap is the whole reason `space_invitations` exists rather than a nullable column on `space_members` — the invited address may have no user row to reference yet.

- Only `token_hash` (sha256 of a 32-byte random token) is stored. The plaintext is returned once at creation, like a share link.
- Re-inviting the same address replaces the pending row, so only the newest link works.
- Accepting requires a signed-in user **whose email matches the invitation**. The link alone is not authority: it is addressed to a person, not to whoever forwards it.
- Inviting an address that already has an account skips the invitation and adds the member directly.

## Anonymous Share Browsing

A link grants a subtree, not a database. Every node id an anonymous caller supplies is walked back up to the share root before anything is served; a node outside the share answers **404 rather than 403**, because confirming that a node exists but sits outside the share is itself more than the visitor should learn. Owner names are stripped from share responses so the page cannot leak who works at the organisation.

## Implementation Notes

- Resolution is a 2-join lookup per request; no permission cache in MVP. If profiling shows it hot, add a request-scoped cache first, then an invalidated cache keyed by `(user, node)`.
- Share tokens: only `token_hash` (sha256) is stored; the plaintext is displayed once at creation. Revocation = row delete. Optional `expires_at`.
- WOPI access tokens encode the resolved permission at session-open time; changing space role mid-session does not retroactively change an open editor session (acceptable; tokens expire).

## Quotas

Per-space `quota_bytes`, designed to be enforced at version-commit time with `used_bytes` maintained asynchronously. Neither half is wired up yet — see the Quota section of [data-model.md](data-model.md).
