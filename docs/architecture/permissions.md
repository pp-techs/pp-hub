# Permissions

## Model

A space **is a company**. One instance hosts several, a person can belong to
more than one, and a space owns exactly one file tree.

Three layers:

1. **Space roles** — `admin` (manage members, groups and settings; holds edit
   everywhere in the space) and `member`. A `member` role grants **no access to
   any file**: it says you belong to the company, nothing more.
2. **Groups** — named sets of users inside one space. Every space has an
   automatic `Everyone` group (`groups.is_everyone`) holding every member; the
   server maintains its membership and it cannot be deleted. It is how someone
   who has been granted nothing individually gets any access at all.
3. **Per-node grants** — a row in `shares`, of kind `group` (to a group),
   `direct` (to one user) or `link` (anonymous token). Permission values:
   `view` | `download` | `edit`.

Access is granted **group → folder**, and inherits down the subtree. A full
per-node ACL was still rejected: a grant names a folder, resolution walks
ancestors, and there is no per-node matrix to invalidate.

## Where It Is Enforced

Every node-addressed route (`/api/v1/nodes/{id}/…`), creating into a folder, and
trash restore/purge resolve through `requireNodeAccess` / `requireParentWrite`.

The surfaces that list many nodes at once cannot use a per-row check without one
recursive walk per result, so they over-fetch and narrow in one batch —
`reachability` in `internal/httpapi/reachability.go`, backed by a single
`NodeAccessRanks` call plus one role lookup per distinct space:

| Surface                              | How it narrows                                           |
| :----------------------------------- | :------------------------------------------------------- |
| `GET /spaces/{id}/children`, `/tree` | one rank check on the **parent**; see below              |
| `GET /search`                        | over-fetch `limit × searchOverfetch`, filter, then limit |
| `GET /starred`, `/recent`            | batch filter                                             |
| `GET /activity`                      | batch filter with its own rule for purged nodes          |
| `GET /spaces/{id}/trash`             | batch filter — the resolver survives tombstones          |
| `GET /shared`                        | `ListEntryPoints`: the grants themselves                 |

**Folder listings need one check, not one per row.** Permission inherits
downward, so if the caller can reach the parent at all, the whole listing is
theirs. Only when the parent resolves to 0 — a member at the space root, or in a
folder nobody granted them — does it narrow to the children they hold a grant on
directly (`ListGrantedChildrenWithMeta`). At the space root that narrowing is
the feature: the top level becomes the list of places their access starts,
rather than the company's entire filing cabinet.

`GET /usage` is deliberately **not** narrowed. It is an aggregate of bytes per
MIME family, names nothing, and the rail already shows the space quota beside
it.

## Resolution Order

Effective permission for a user on a node:

1. The **space-role floor**: `admin` → 3 (edit), `member` → **0**.
2. The highest grant on the node **or any ancestor**, whether granted directly
   to the user or to a group they belong to, ignoring expired rows.
   **Grants only widen, never narrow.**
3. Link share token (no authenticated user) → link permission, clamped to the
   read paths (`view`/`download`). Edit-via-link is post-MVP.

Steps 1 and 2 combine with `max`. For a member, step 1 contributes nothing, so
their permission is entirely whatever has been granted.

`share.AccessRanks` resolves step 2 for **many nodes in one query**
(`NodeAccessRanks`), so a listing can filter itself without a per-row check.
This replaced an ancestor walk that issued one `GetNodeParent` round trip per
level per node and silently stopped widening at depth 100.

That query's recursive join deliberately does **not** filter `deleted_at`.
`GetNodeParent` does, which truncated the chain at the first trashed ancestor
and lost whatever grant sat above it — the reason trash could not use share
resolution at all and fell back to a weaker space-role check. A tombstone now
resolves exactly like a live node.

## Rank Mapping

`requireNodeAccess(minRank)` compares an _effective rank_ against the rank a
route asks for, on the share scale: `view=1`, `download=2`, `edit=3`. The space
role maps onto that same scale through `roleFloor`:

| Space role | Floor rank |
| :--------- | :--------- |
| admin      | 3 (edit)   |
| member     | 0 (none)   |

This is the **single seam** where "is a member" could become implicit access,
and it is deliberately the only copy of it. The WOPI handler used to keep its
own switch on raw role strings, which drifted to `editor → 2` while
`requireNodeAccess` used `editor → 3`: a user passed the rank-3 gate and was
then handed a token that `share.CanWrite` (rank ≥ 3) rejected, so an editor got
a read-only editing session. Switching on role strings is what let the two
diverge without the compiler noticing.

`spaces.roleRank` is a **different, positional** scale (member 1 < admin 2) used
only by `RequireRole` for role-vs-role comparison. Keep the two apart.

Admin-only powers — member management, space settings, permanent deletion,
emptying the trash — are gated by `RequireRole(RoleAdmin)` at the space level
and never travel through the permission scale.

## Writing Into a Folder

`POST /spaces/{spaceID}/dirs` and `POST /spaces/{spaceID}/files` are keyed by
space, and take `parent_id` from the request body. Nothing used to check it —
not that it belonged to `spaceID`, not that the caller could write there — which
was harmless only while a space editor could write anywhere in their space.

`requireParentWrite` now resolves the parent and demands rank 3 on it. A parent
in another space answers **404, not 403**: confirming that a node exists is
already more than the caller should learn, the same rule anonymous share
browsing follows. A nil parent means the space root, which nobody holds a grant
on, so creating at the top level is admin-only.

## Migrating From the Old Role Scale

Migration `0011_groups.sql` collapses `owner|editor|viewer` to `admin|member`
and hands the lost access back as explicit grants, so that **every user's
effective permission on every node is unchanged** by the migration. Per space it
creates:

- `Everyone` (`is_everyone`) holding every non-owner member, granted **`view`**
  on every root node.
- `Editors (migrated)` holding the former editors, granted **`edit`** on every
  root node. Only created where the space actually had editors.

Two groups, not one, and that is the point: a single `Everyone` group carrying
`edit` would have handed former _viewers_ edit — an escalation, not a
migration. `Editors (migrated)` is safe to delete once real groups are set up.

Tombstoned roots get their grants too, so a restore brings access back with it.

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

## Group Membership Invariants

Enforced in `spaces.Service`, not by the database:

- **Creating a space creates its `Everyone` group.** `groups_everyone_uniq`
  assumes one exists; migration `0011` created one for every pre-existing space
  and `spaces.Create` keeps new spaces at parity.
- **Adding a member adds them to `Everyone`.**
- **Removing a member removes them from every group in that space.**
  `group_members` references `users`, not `spaces`, so nothing cascades when the
  `space_members` row goes. Skipping this leaves someone who is no longer in the
  company still holding live access through their old groups — the easiest
  invariant here to forget and the most damaging to miss.

## Implementation Notes

- Resolution is one recursive query per request (`NodeAccessRanks`), batched across nodes; no permission cache in MVP. If profiling shows it hot, add a request-scoped cache first, then an invalidated cache keyed by `(user, node)`.
- Share tokens: only `token_hash` (sha256) is stored; the plaintext is displayed once at creation. Revocation = row delete. Optional `expires_at`.
- WOPI access tokens encode the resolved permission at session-open time; changing space role mid-session does not retroactively change an open editor session (acceptable; tokens expire).

## Quotas

Per-space `quota_bytes`, designed to be enforced at version-commit time with `used_bytes` maintained asynchronously. Neither half is wired up yet — see the Quota section of [data-model.md](data-model.md).
