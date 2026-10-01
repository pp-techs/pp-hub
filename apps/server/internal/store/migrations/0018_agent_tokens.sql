-- Personal access tokens for agents (M41).
--
-- Chat apps reach /mcp/ through OAuth with Logto, and their tokens are
-- Logto's. A coding agent configured from a file (.mcp.json), a CI job or a
-- script has no browser to sign in with, so a person can also mint a token
-- here and paste it into an Authorization header. It acts for its creator
-- with exactly their access, and only on /mcp/.
--
-- Stored like a session: only the SHA-256 of the token, so a dump of this
-- table yields nothing a client can present. Unlike a session it does not
-- slide: expires_at is fixed at creation, at most 90 days out, so a token
-- that leaks stops working on a date its owner chose.
CREATE TABLE agent_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   BYTEA NOT NULL UNIQUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ NOT NULL
);

-- A person's token list.
CREATE INDEX agent_tokens_user_id ON agent_tokens (user_id, created_at DESC);
-- The expiry sweep.
CREATE INDEX agent_tokens_expires_at ON agent_tokens (expires_at);
