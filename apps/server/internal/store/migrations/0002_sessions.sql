-- Server-side sessions.
--
-- The cookie used to be self-contained — "v1.<uuid>.<expiry>.<hmac>" — which
-- meant there was nothing to revoke. A stolen cookie stayed valid for its full
-- thirty days, "sign out" only cleared the browser's copy, and the one way to
-- invalidate anything was to rotate PP_SESSION_KEY, which signed everybody out
-- at once. It also tied session validity to a secret that the worker needs for
-- an unrelated purpose (WOPI tokens), so the two could never be rotated apart.
--
-- The cookie is now an opaque 256-bit random token and this table is the
-- authority. Only the SHA-256 of the token is stored: a dump of this table
-- does not yield a usable cookie, the same reason password hashes exist.
-- The lookup is by hash, so it stays a single indexed read.
CREATE TABLE sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BYTEA NOT NULL UNIQUE,
    user_agent   TEXT NOT NULL DEFAULT '',
    ip           TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Idle deadline, pushed forward as the session is used. The absolute cap
    -- is enforced against created_at, so a session cannot be kept alive
    -- indefinitely by traffic alone.
    expires_at   TIMESTAMPTZ NOT NULL
);

-- Listing and bulk-revoking a person's sessions.
CREATE INDEX sessions_user_id ON sessions (user_id, last_used_at DESC);
-- The expiry sweep.
CREATE INDEX sessions_expires_at ON sessions (expires_at);
