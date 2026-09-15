-- name: CreateGroup :one
INSERT INTO groups (space_id, name, description, is_everyone, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetEveryoneGroup :one
-- Every space has exactly one, enforced by groups_everyone_uniq.
SELECT * FROM groups WHERE space_id = $1 AND is_everyone LIMIT 1;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id)
VALUES ($1, $2)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveUserFromSpaceGroups :exec
-- Removing someone from a space must drop them from every group in it.
-- group_members references users, not spaces, so there is no cascade to rely
-- on: without this the person keeps live access through their old groups.
DELETE FROM group_members gm
USING groups g
WHERE gm.group_id = g.id
  AND g.space_id = $1
  AND gm.user_id = $2;
