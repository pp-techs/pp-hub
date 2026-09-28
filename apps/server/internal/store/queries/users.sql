-- name: CreateUser :one
INSERT INTO users (oidc_sub, oidc_iss, email, name)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByIdentity :one
SELECT * FROM users WHERE oidc_iss = $1 AND oidc_sub = $2;

-- Refreshes the profile fields the IdP owns on every sign-in.
-- name: TouchUserLogin :one
UPDATE users SET email = $2, name = $3, last_seen = now()
WHERE id = $1
RETURNING *;

-- The same IdP-owned fields, refreshed from the Management API rather than
-- from a sign-in, so last_seen is left alone: nobody was seen.
-- name: SetUserIdpIdentity :one
UPDATE users SET email = $2, name = $3
WHERE id = $1
RETURNING *;

-- Every account this identity provider issued, for the hourly account sweep.
-- name: ListUsersByIssuer :many
SELECT * FROM users WHERE oidc_iss = $1;

-- Re-points an existing account at a new (issuer, subject) pair.
--
-- Identity is keyed on (oidc_iss, oidc_sub), and oidc_iss is a URL an operator
-- can change — moving the IdP behind a new hostname, or setting
-- PP_OIDC_ISSUER_EXTERNAL for the first time. Without this, that change made
-- every returning user look brand new, the insert collided with the unique
-- index on (tenant_id, email_hash), and sign-in answered 500 with no way back
-- short of hand-editing Postgres.
-- name: AdoptUserIdentity :one
UPDATE users SET oidc_iss = $2, oidc_sub = $3, email = $4, name = $5, last_seen = now()
WHERE id = $1
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1 LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2, bio = $3, settings = COALESCE($4, settings)
WHERE id = $1
RETURNING *;

-- Preferences are their own statement rather than a fourth argument to
-- UpdateUserProfile: that one is a full replace of display_name and bio, so
-- routing a language change through it would make "change your language"
-- capable of blanking a bio written from another tab.
-- name: UpdateUserSettings :one
UPDATE users SET settings = $2 WHERE id = $1 RETURNING *;

-- Avatar writes are conditional on the blob the caller read (@prev_blob) and
-- answer no row when it has moved: only the writer that actually replaced a
-- blob may release its reference. Two unguarded writers would both release
-- it, and in a deduplicating CAS that blob can be a file's content as well.

-- A person's own upload. Clears avatar_from_idp so a later IdP sync that
-- drops its picture does not take this one with it.
-- name: SetUserAvatar :one
UPDATE users
SET avatar_url = @avatar_url, avatar_blob = @avatar_blob, avatar_mime = @avatar_mime,
    avatar_from_idp = false
WHERE id = @id AND avatar_blob = @prev_blob
RETURNING *;

-- The avatar mirrored from the IdP's `picture`, together with the source URL
-- it came from. An empty blob clears an IdP-supplied avatar.
-- name: SetUserIdpAvatar :one
UPDATE users
SET avatar_url = @avatar_url, avatar_blob = @avatar_blob, avatar_mime = @avatar_mime,
    avatar_from_idp = @avatar_blob::text <> '', idp_picture = @idp_picture
WHERE id = @id AND avatar_blob = @prev_blob
RETURNING *;

-- Records the IdP's `picture` as seen without touching the avatar.
-- name: SetUserIdpPicture :one
UPDATE users SET idp_picture = $2 WHERE id = $1 RETURNING *;

-- name: SetUserIdpDetails :one
UPDATE users SET phone = $2, gender = $3 WHERE id = $1 RETURNING *;

-- name: GetUserAvatar :one
SELECT avatar_blob, avatar_mime FROM users WHERE id = $1;

-- name: SearchUsers :many
-- Autocomplete over accounts the caller may legitimately see. A space is a
-- company, so an ordinary caller only matches people they already share a
-- space with; @unrestricted lifts that for an instance admin, whose console
-- lists every account anyway.
SELECT u.id, u.email, u.name, u.display_name, u.avatar_url
FROM users u
WHERE u.tenant_id = @tenant_id
  AND (
    u.email ILIKE '%' || @q::text || '%'
    OR u.name ILIKE '%' || @q::text || '%'
    OR u.display_name ILIKE '%' || @q::text || '%'
  )
  AND (
    @unrestricted::bool
    OR u.id = @caller_id
    OR EXISTS (
      SELECT 1
      FROM space_members mine
      JOIN space_members theirs ON theirs.space_id = mine.space_id
      WHERE mine.user_id = @caller_id AND theirs.user_id = u.id
    )
  )
ORDER BY
  CASE
    WHEN u.email ILIKE @q::text || '%' THEN 0
    WHEN u.display_name ILIKE @q::text || '%' THEN 1
    WHEN u.name ILIKE @q::text || '%' THEN 2
    ELSE 3
  END,
  u.display_name, u.email
LIMIT @max_rows;

-- name: ListUsers :many
SELECT * FROM users
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE tenant_id = $1;

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = $2 WHERE id = $1;

-- Who a security alert is mailed to: every live instance admin with an
-- address. No opt-out — alerts are turned off instance-wide, not per person.
-- name: ListInstanceAdminRecipients :many
SELECT id, email, settings FROM users
WHERE is_admin AND suspended_at IS NULL AND email <> '';

-- name: GetSpaceMembers :many
SELECT m.user_id, m.created_at, u.email, u.name, u.display_name, u.avatar_url,
  EXISTS (
    SELECT 1 FROM group_members gm JOIN groups g ON g.id = gm.group_id
    WHERE g.space_id = m.space_id AND g.is_admin AND gm.user_id = m.user_id
  ) AS is_admin
FROM space_members m
JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY m.created_at;

-- name: IsSpaceMember :one
SELECT EXISTS(SELECT 1 FROM space_members WHERE space_id = $1 AND user_id = $2);

