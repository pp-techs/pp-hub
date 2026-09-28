-- name: RecordSecurityEvent :exec
INSERT INTO security_events (kind, user_id, subject, ip, user_agent, request_id, detail, node_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- An event no request caused — an alert the watcher raised, or an account
-- change the identity provider reported. There is no user agent or request id
-- to attach; the address is the one the event is about, and the user is the
-- account it concerns when there is one.
-- name: RecordSystemSecurityEvent :exec
INSERT INTO security_events (kind, user_id, subject, ip, detail)
VALUES (@kind, @user_id, @subject, @ip, @detail);

-- The console's tail. The actor's email is joined in so a row still reads as a
-- person rather than a UUID, and falls back to the recorded subject when the
-- account is gone. The node's name falls back the same way, to the name
-- recorded when the event was written.
--
-- A node filter matches the node and everything under it. The walk includes
-- trashed descendants on purpose — a file in the trash was still read — and
-- the direct `e.node_id = @node` arm still matches a node purged from `nodes`.
-- name: ListSecurityEvents :many
WITH RECURSIVE scope AS (
  SELECT n.id FROM nodes n WHERE n.id = sqlc.narg(node)::uuid
  UNION ALL
  SELECT c.id FROM nodes c JOIN scope ON c.parent_id = scope.id
)
SELECT
  e.id, e.kind, e.user_id, e.subject, e.ip, e.user_agent, e.request_id,
  e.detail, e.created_at, e.node_id,
  COALESCE(u.email, '') AS user_email,
  COALESCE(u.display_name, u.name, '') AS user_name,
  COALESCE(n.name, e.detail->>'name', '')::text AS node_name
FROM security_events e
LEFT JOIN users u ON u.id = e.user_id
LEFT JOIN nodes n ON n.id = e.node_id
WHERE (sqlc.narg(kind)::text IS NULL OR e.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(node)::uuid IS NULL OR e.node_id = sqlc.narg(node)::uuid
       OR e.node_id IN (SELECT id FROM scope))
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR e.created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR e.created_at < sqlc.narg(to_at)::timestamptz)
ORDER BY e.created_at DESC, e.id DESC
LIMIT $1 OFFSET $2;

-- name: CountSecurityEvents :one
WITH RECURSIVE scope AS (
  SELECT n.id FROM nodes n WHERE n.id = sqlc.narg(node)::uuid
  UNION ALL
  SELECT c.id FROM nodes c JOIN scope ON c.parent_id = scope.id
)
SELECT COUNT(*) FROM security_events e
WHERE (sqlc.narg(kind)::text IS NULL OR e.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(node)::uuid IS NULL OR e.node_id = sqlc.narg(node)::uuid
       OR e.node_id IN (SELECT id FROM scope))
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR e.created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR e.created_at < sqlc.narg(to_at)::timestamptz);

-- The CSV export: the console's filters, paged by keyset rather than offset
-- so a long export neither skips nor repeats rows while new ones arrive. The
-- select list must stay identical to ListSecurityEvents' — the handler
-- converts one row type into the other.
-- name: ListSecurityEventsExport :many
WITH RECURSIVE scope AS (
  SELECT n.id FROM nodes n WHERE n.id = sqlc.narg(node)::uuid
  UNION ALL
  SELECT c.id FROM nodes c JOIN scope ON c.parent_id = scope.id
)
SELECT
  e.id, e.kind, e.user_id, e.subject, e.ip, e.user_agent, e.request_id,
  e.detail, e.created_at, e.node_id,
  COALESCE(u.email, '') AS user_email,
  COALESCE(u.display_name, u.name, '') AS user_name,
  COALESCE(n.name, e.detail->>'name', '')::text AS node_name
FROM security_events e
LEFT JOIN users u ON u.id = e.user_id
LEFT JOIN nodes n ON n.id = e.node_id
WHERE (sqlc.narg(kind)::text IS NULL OR e.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(node)::uuid IS NULL OR e.node_id = sqlc.narg(node)::uuid
       OR e.node_id IN (SELECT id FROM scope))
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR e.created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR e.created_at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (e.created_at, e.id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg(lim)::int;

-- Retention. Security rows are small but they are written on every sign-in, so
-- something has to bound them; the sweep that purges expired sessions does
-- this too. Reads have their own, usually shorter, retention, so each
-- statement leaves the other's kinds alone.
-- name: DeleteSecurityEventsBefore :execrows
DELETE FROM security_events
WHERE created_at < @before AND kind <> ALL(@read_kinds::text[]);

-- name: DeleteReadEventsBefore :execrows
DELETE FROM security_events
WHERE created_at < @before AND kind = ANY(@read_kinds::text[]);

-- Addresses with at least @threshold failed sign-ins since @since. An empty
-- address is a request the server could not place, not one source.
-- name: FailedLoginBursts :many
SELECT ip, COUNT(*)::bigint AS attempts,
  MIN(created_at)::timestamptz AS first_at, MAX(created_at)::timestamptz AS last_at,
  array_agg(DISTINCT subject)::text[] AS codes
FROM security_events
WHERE kind = 'login_failed' AND created_at >= @since AND ip <> ''
GROUP BY ip HAVING COUNT(*) >= @threshold::bigint
ORDER BY attempts DESC;

-- Addresses already alerted on for @rule since @since, so a burst raises one
-- alert per window rather than one per scan.
-- name: RecentSecurityAlertIPs :many
SELECT DISTINCT ip FROM security_events
WHERE kind = 'security_alert' AND subject = @rule AND created_at >= @since;
