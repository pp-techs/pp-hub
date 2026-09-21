-- Logto organizations become the source of truth for space membership.
--
-- See docs/architecture/logto-org-sync.md. Two structural changes and one
-- removal:
--
--  1. A space can name the organization it mirrors. Nullable, because a space
--     created before the link exists is still a space — it simply has no
--     upstream and nothing reconciles it.
--  2. Departures are a queue. Membership revocation is immediate and automatic
--     (the sync path deletes the rows); the destructive half of the old
--     RemoveMember — grant teardown, ownership climb, the move into Archive —
--     waits for an admin to confirm, because an accidental click in the Logto
--     console must not be able to trigger it.
--  3. space_invitations goes. An invitation now lives in Logto, which owns the
--     address, the token and the email that carries them.

ALTER TABLE spaces ADD COLUMN idp_org_id TEXT;

-- Partial, so any number of spaces may stay unlinked while a linked one is
-- unique: two spaces mirroring one organization would each revoke the other's
-- members on every reconcile.
CREATE UNIQUE INDEX spaces_idp_org_uniq ON spaces (idp_org_id) WHERE idp_org_id IS NOT NULL;

CREATE TABLE space_departures (
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- How we learned: signin | webhook | reconcile | admin. TEXT rather than
    -- an enum for the same reason audit.Kind is: adding a source is a code
    -- change, not a migration.
    source      TEXT NOT NULL,
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    PRIMARY KEY (space_id, user_id)
);

-- The only query anyone runs against this table on a hot path: "what is still
-- waiting for an admin in this space".
CREATE INDEX space_departures_pending ON space_departures (space_id) WHERE resolved_at IS NULL;

DROP TABLE space_invitations;
