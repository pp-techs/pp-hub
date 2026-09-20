-- An audit trail for the things that change who can do what.
--
-- `activity_events` cannot hold these. It is a per-space feed — `space_id` is
-- NOT NULL and references `spaces` — and an authentication event belongs to no
-- space. Worse, the interesting authentication event is the *failed* one,
-- where there may be no local account to point `actor_id` at, and
-- `activity_events.actor_id` is NOT NULL too. They are also read by different
-- people for different reasons: one is a feed of work for colleagues, this is
-- a record of access for whoever has to answer "who got in, from where".
--
-- So: no space, a nullable user, and the request context that the other table
-- has never carried.
CREATE TABLE security_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       TEXT NOT NULL,
    -- Null when no account was resolved: a sign-in that failed before
    -- identity, or an account deleted since. ON DELETE SET NULL rather than
    -- CASCADE on purpose — deleting a user must not erase the record that
    -- they signed in, which is the whole point of keeping one.
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    -- Who the caller *claimed* to be when user_id is null, and a stable label
    -- for the row after the account is gone. Free text: it is an email, an
    -- OIDC subject or an error code depending on the event.
    subject    TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    -- chi's RequestID, so a row can be joined to the access log line.
    request_id TEXT NOT NULL DEFAULT '',
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The console reads the tail, newest first.
CREATE INDEX security_events_recent ON security_events (created_at DESC);
-- "Everything this account did" and "every failed sign-in from this address"
-- are the two questions an incident actually asks.
CREATE INDEX security_events_by_user ON security_events (user_id, created_at DESC)
    WHERE user_id IS NOT NULL;
CREATE INDEX security_events_by_kind ON security_events (kind, created_at DESC);
