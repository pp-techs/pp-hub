-- name: LockGuestOTP :one
-- Read for an update inside the transaction that decides whether a code may
-- be sent or checked, so two requests cannot both spend the same allowance.
SELECT * FROM guest_otps WHERE email_norm = $1 FOR UPDATE;

-- name: PutGuestOTP :exec
-- Stores a fresh code for an address and counts the send against the
-- address's hourly window. A window that has run out starts again.
INSERT INTO guest_otps (email_norm, code_hash, expires_at, sends, window_start)
VALUES (@email_norm, @code_hash, @expires_at, 1, now())
ON CONFLICT (email_norm) DO UPDATE SET
  code_hash    = EXCLUDED.code_hash,
  expires_at   = EXCLUDED.expires_at,
  attempts     = 0,
  created_at   = now(),
  sends        = CASE WHEN guest_otps.window_start > now() - interval '1 hour'
                      THEN guest_otps.sends + 1 ELSE 1 END,
  window_start = CASE WHEN guest_otps.window_start > now() - interval '1 hour'
                      THEN guest_otps.window_start ELSE now() END;

-- name: BumpGuestOTPAttempts :one
UPDATE guest_otps SET attempts = attempts + 1 WHERE email_norm = $1 RETURNING attempts;

-- name: ExpireGuestOTP :exec
-- A spent or burnt code: the row stays so the send window is remembered, but
-- the code can no longer be presented.
UPDATE guest_otps SET expires_at = now() - interval '1 second', attempts = 0 WHERE email_norm = $1;

-- name: PruneGuestOTPs :execrows
DELETE FROM guest_otps WHERE window_start < now() - interval '1 hour' AND expires_at < now();

-- name: GuestHasLiveShare :one
-- Whether the guest holding an address still has a grant that has not
-- expired: the other reason a guest code is sent to it.
SELECT EXISTS (
  SELECT 1 FROM shares s JOIN users u ON u.id = s.grantee_user_id
  WHERE u.kind = 'guest' AND lower(u.email) = @email_norm::text
    AND s.kind = 'direct' AND (s.expires_at IS NULL OR s.expires_at > now())
)::boolean AS live;
