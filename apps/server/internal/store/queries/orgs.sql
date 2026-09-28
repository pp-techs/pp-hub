-- name: SetSpaceOrg :exec
-- Link a space to the Logto organization it mirrors, or unlink it with NULL.
-- Separate from CreateSpace because the link is set by whichever path
-- discovered or created the organization, after the space row exists.
UPDATE spaces SET idp_org_id = $2 WHERE id = $1;

-- name: SetSpaceName :exec
-- The organization's name, mirrored by reconcile.
UPDATE spaces SET name = $2 WHERE id = $1;

-- name: DetachOrg :exec
-- Remember that this organization's space was deleted here, so sign-in does
-- not recreate it.
INSERT INTO detached_orgs (org_id, detached_by) VALUES ($1, $2)
ON CONFLICT (org_id) DO NOTHING;

-- name: IsOrgDetached :one
SELECT EXISTS (SELECT 1 FROM detached_orgs WHERE org_id = $1);

-- name: ReattachOrg :exec
DELETE FROM detached_orgs WHERE org_id = $1;

-- name: ListSpaceMemberAccounts :many
-- Every member's account, identity key included, for pushing a space's
-- membership up to a new organization.
SELECT u.* FROM space_members m JOIN users u ON u.id = m.user_id
WHERE m.space_id = $1
ORDER BY u.email;

-- name: GetSpaceByOrgID :one
SELECT * FROM spaces WHERE idp_org_id = $1 LIMIT 1;

-- name: ListLinkedSpaces :many
-- Every space with an upstream organization, for the periodic reconcile. The
-- unlinked ones have nothing to compare against and are skipped entirely
-- rather than reconciled to empty.
SELECT * FROM spaces WHERE idp_org_id IS NOT NULL ORDER BY created_at;

-- name: ListSpaceMemberIDs :many
-- The local membership set, for diffing against the organization's. The
-- detailed listings join users and compute per-member figures; reconcile wants
-- neither.
SELECT user_id FROM space_members WHERE space_id = $1;

-- name: RecordDeparture :exec
-- Queue a confirmed departure for handover.
--
-- Re-departing after a previous handover reopens the row rather than colliding
-- with it: somebody can leave, be re-added, and leave again, and the second
-- departure needs an admin as much as the first did.
INSERT INTO space_departures (space_id, user_id, source)
VALUES ($1, $2, $3)
ON CONFLICT (space_id, user_id) DO UPDATE
SET detected_at = now(), source = EXCLUDED.source, resolved_at = NULL, resolved_by = NULL;

-- name: ClearDeparture :exec
-- Rejoining cancels a pending handover. Their content never moved — that is
-- the whole point of deferring it — so there is nothing to undo, only a queue
-- entry that no longer describes reality.
DELETE FROM space_departures WHERE space_id = $1 AND user_id = $2 AND resolved_at IS NULL;

-- name: ListPendingDepartures :many
SELECT d.space_id, d.user_id, d.detected_at, d.source,
       u.email, u.name AS user_name, u.display_name, u.avatar_url,
       (SELECT COUNT(*) FROM nodes n
         WHERE n.space_id = d.space_id AND n.created_by = d.user_id
           AND n.deleted_at IS NULL AND n.kind = 'file')::bigint AS files_owned
FROM space_departures d
JOIN users u ON u.id = d.user_id
WHERE d.space_id = $1 AND d.resolved_at IS NULL
ORDER BY d.detected_at;

-- name: GetPendingDeparture :one
SELECT * FROM space_departures
WHERE space_id = $1 AND user_id = $2 AND resolved_at IS NULL
LIMIT 1;

-- name: ResolveDeparture :exec
UPDATE space_departures SET resolved_at = now(), resolved_by = $3
WHERE space_id = $1 AND user_id = $2 AND resolved_at IS NULL;
