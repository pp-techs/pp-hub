-- name: CreateSpace :one
INSERT INTO spaces (name, owner_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetSpace :one
SELECT * FROM spaces WHERE id = $1 LIMIT 1;

-- name: ListMySpaces :many
-- The personal space leads: it is the one every account has (when the instance
-- provisions them), and the place a person lands. An archived personal space
-- belongs to an account Logto no longer has; it is listed for nobody.
SELECT s.* FROM spaces s
JOIN space_members m ON m.space_id = s.id
WHERE m.user_id = $1
  AND (s.kind <> 'personal' OR s.archived_at IS NULL)
ORDER BY (s.kind = 'personal') DESC, s.created_at;

-- name: CreatePersonalSpace :one
-- At most one per owner (spaces_personal_owner_uniq). A concurrent sign-in
-- that lost the race gets no row and reads the winner's instead.
INSERT INTO spaces (name, owner_id, kind, external_sharing, quota_bytes)
VALUES (@name, @owner_id, 'personal', true, @quota_bytes)
ON CONFLICT (owner_id) WHERE kind = 'personal' DO NOTHING
RETURNING *;

-- name: GetPersonalSpace :one
SELECT * FROM spaces WHERE owner_id = $1 AND kind = 'personal' LIMIT 1;

-- name: ArchivePersonalSpace :exec
-- The owner's account is gone upstream: nobody reaches the space again, and an
-- instance administrator purges it.
UPDATE spaces SET archived_at = now()
WHERE owner_id = $1 AND kind = 'personal' AND archived_at IS NULL;

-- name: CountOrgSpaces :one
-- Organization spaces the person belongs to — whether `personal_spaces =
-- members` admits them.
SELECT COUNT(*)::bigint FROM space_members m
JOIN spaces s ON s.id = m.space_id
WHERE m.user_id = $1 AND s.kind = 'org';

-- name: ListArchivedPersonalSpaces :many
-- Personal spaces whose owner Logto no longer has, for an instance admin to
-- purge.
SELECT s.id, s.name, s.used_bytes, s.archived_at, s.created_at, u.email AS owner_email
FROM spaces s
JOIN users u ON u.id = s.owner_id
WHERE s.kind = 'personal' AND s.archived_at IS NOT NULL
ORDER BY s.archived_at;

-- name: AddSpaceMember :exec
-- Membership only: there is no role column anymore (M15). Admin authority is
-- Admin-group membership, maintained separately.
INSERT INTO space_members (space_id, user_id) VALUES ($1, $2)
ON CONFLICT (space_id, user_id) DO NOTHING;

-- name: RemoveSpaceMember :exec
DELETE FROM space_members WHERE space_id = $1 AND user_id = $2;

-- name: ListSpaceMembers :many
SELECT m.space_id, m.user_id, m.created_at, u.email, u.name AS user_name, u.display_name, u.avatar_url,
  EXISTS (
    SELECT 1 FROM group_members gm JOIN groups g ON g.id = gm.group_id
    WHERE g.space_id = m.space_id AND g.is_admin AND gm.user_id = m.user_id
  ) AS is_admin
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;


-- name: SetArchiveRoot :exec
-- Where a departed member's content lands. Created at space creation, like
-- the Everyone/Admin groups.
UPDATE spaces SET archive_root_id = $2 WHERE id = $1;

-- name: SetSpaceAvatar :one
-- A space's avatar. The swap is conditional on the blob the space held when
-- the write started, so of two concurrent uploads exactly one lands and the
-- other gets no row (→ ErrAvatarConflict). An empty blob clears the avatar;
-- the trigger on spaces moves the reference either way.
UPDATE spaces
SET avatar_blob = @avatar_blob, avatar_mime = @avatar_mime
WHERE id = @id AND avatar_blob = @prev_blob
RETURNING *;

-- name: GetSpaceAvatar :one
SELECT avatar_blob, avatar_mime FROM spaces WHERE id = $1;

-- name: ListSpaceMembersDetailed :many
-- Members with the two figures the settings table shows that a plain member
-- row cannot: how many live files they own here, and when they were last seen.
SELECT
  m.space_id, m.user_id, m.created_at,
  u.email, u.name AS user_name, u.display_name, u.avatar_url, u.last_seen,
  (SELECT COUNT(*) FROM nodes n
    WHERE n.space_id = m.space_id AND n.created_by = m.user_id
      AND n.deleted_at IS NULL AND n.kind = 'file')::bigint AS files_owned,
  EXISTS (
    SELECT 1 FROM group_members gm JOIN groups g ON g.id = gm.group_id
    WHERE g.space_id = m.space_id AND g.is_admin AND gm.user_id = m.user_id
  ) AS is_admin
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;
