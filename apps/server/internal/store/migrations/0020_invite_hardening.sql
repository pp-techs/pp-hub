-- Hardening for M42's sharing by email (docs/architecture/personal-spaces.md).
--
-- users.email_verified: whether the stored address is one the identity
-- provider vouched for. Sharing by email resolves an address to an account,
-- and an account created from an unverified claim could otherwise collect what
-- was meant for the address's real owner. Existing rows start unverified; a
-- verified sign-in or the hourly Logto account sweep marks them.
ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT false;

-- Every invitation and external grant handed out, append-only. The budget
-- counts this rather than live rows, so revoking and re-inviting does not
-- refund it, and it caps how many invitation mails one address receives.
-- Pruned after two days by the server's sweep.
CREATE TABLE invitation_log (
    id           BIGSERIAL PRIMARY KEY,
    created_by   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient    TEXT NOT NULL,                -- normalised address
    mailed       BOOLEAN NOT NULL,             -- an invitation mail was sent
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX invitation_log_by_creator ON invitation_log (created_by, created_at);
CREATE INDEX invitation_log_by_recipient ON invitation_log (recipient, created_at) WHERE mailed;
CREATE INDEX invitation_log_created_at ON invitation_log (created_at);

-- The live-row indexes the old budget used.
DROP INDEX shares_external_by_creator;
DROP INDEX pending_shares_by_creator;
