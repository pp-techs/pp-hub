-- name: UpsertPendingShare :one
-- One invitation per (node, address). Inviting the same address again
-- replaces the level, flags, expiry and inviter rather than stacking rows;
-- `inserted` tells a new invitation (mail, rate limit) from a change.
INSERT INTO pending_shares (node_id, email_norm, level, can_download, created_by, expires_at)
VALUES (@node_id, @email_norm, @level, @can_download, @created_by, @expires_at)
ON CONFLICT (node_id, email_norm)
DO UPDATE SET level = EXCLUDED.level, can_download = EXCLUDED.can_download,
              created_by = EXCLUDED.created_by, expires_at = EXCLUDED.expires_at
RETURNING *, (xmax = 0)::bool AS inserted;

-- name: GetPendingShareOnNode :one
SELECT * FROM pending_shares WHERE id = $1 AND node_id = $2 LIMIT 1;

-- name: ListPendingSharesForNode :many
SELECT * FROM pending_shares
WHERE node_id = $1 AND (expires_at IS NULL OR expires_at > now())
ORDER BY created_at DESC;

-- name: DeletePendingShare :execrows
-- The node is part of the predicate for the same reason as DeleteShare.
DELETE FROM pending_shares WHERE id = $1 AND node_id = $2;

-- name: RecordInvitation :exec
-- One external grant or invitation handed out. Append-only: revoking what it
-- created does not remove the row, so the budget cannot be refunded.
INSERT INTO invitation_log (created_by, recipient, mailed) VALUES (@created_by, @recipient, @mailed);

-- name: CountInvitationsBySender :one
-- The sender's budget: everything handed to people outside a space since a
-- cutoff.
SELECT COUNT(*)::bigint FROM invitation_log
WHERE created_by = @created_by AND created_at >= @since::timestamptz;

-- name: CountInvitationMailsTo :one
-- Invitation mails one address received since a cutoff, from anybody — the
-- cap that keeps the instance from being pointed at somebody's inbox.
SELECT COUNT(*)::bigint FROM invitation_log
WHERE recipient = @recipient AND mailed AND created_at >= @since::timestamptz;

-- name: PruneInvitationLog :execrows
DELETE FROM invitation_log WHERE created_at < @before::timestamptz;

-- name: TakePendingShares :many
-- Deletes every invitation for an address — expired ones included — and
-- returns them with the node they name, for adoption at a verified sign-in.
-- The caller decides which become grants: only those whose inviter can still
-- give them. view_only takes only the view invitations and leaves the rest
-- waiting: a guest can hold nothing wider, and an edit invitation is for the
-- address's own sign-in.
WITH taken AS (
    DELETE FROM pending_shares p
    WHERE p.email_norm = @email_norm
      AND (NOT @view_only::bool OR p.level = 'view')
    RETURNING p.*
)
SELECT t.id, t.node_id, t.level, t.can_download, t.created_by, t.expires_at,
       n.space_id, n.name AS node_name
FROM taken t
JOIN nodes n ON n.id = t.node_id;

-- name: InsertAdoptedShare :one
-- A grant made from an invitation. One the person already holds on the node
-- is kept as it is (no row returned): it is the more recent decision.
INSERT INTO shares (node_id, kind, grantee_user_id, level, can_download, can_share, expires_at, created_by, is_external)
VALUES (@node_id, 'direct', @grantee_user_id, @level, @can_download, false, @expires_at, @created_by, @is_external)
ON CONFLICT (node_id, grantee_user_id) WHERE kind = 'direct' DO NOTHING
RETURNING *;

-- name: HasLivePendingShareFor :one
-- Whether an address has a view invitation that has not expired: one of the
-- two reasons a guest code is sent to it.
SELECT EXISTS (
  SELECT 1 FROM pending_shares
  WHERE email_norm = @email_norm AND level = 'view'
    AND (expires_at IS NULL OR expires_at > now())
)::boolean AS live;
