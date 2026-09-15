-- M9: Space settings and invitations.

-- Spaces gain the fields the settings screen edits. slug is the human-readable
-- identifier a share URL could use; it is unique per tenant among live spaces
-- and left empty for spaces created before this migration rather than
-- backfilled from the name, which would collide for duplicate names.
ALTER TABLE spaces ADD COLUMN slug        TEXT NOT NULL DEFAULT '';
ALTER TABLE spaces ADD COLUMN description TEXT NOT NULL DEFAULT '';
-- Role given to someone who joins without one being specified.
ALTER TABLE spaces ADD COLUMN default_role member_role NOT NULL DEFAULT 'viewer';
ALTER TABLE spaces ADD COLUMN archived_at TIMESTAMPTZ;

CREATE UNIQUE INDEX spaces_slug_uniq ON spaces (tenant_id, slug) WHERE slug <> '';

-- An invitation is a promise of membership to an email address that may not
-- have an account yet, which is exactly why it cannot live in space_members:
-- that table's user_id references a user row that does not exist.
CREATE TABLE space_invitations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    email       TEXT NOT NULL,
    role        member_role NOT NULL DEFAULT 'viewer',
    -- sha256 of the token handed out; the plaintext is shown once, like a
    -- share link. Never stored.
    token_hash  TEXT NOT NULL,
    invited_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One live invitation per email per space; re-inviting replaces rather than
-- accumulates. Accepted rows are kept as history and excluded from the index.
CREATE UNIQUE INDEX space_invitations_pending
    ON space_invitations (space_id, lower(email)) WHERE accepted_at IS NULL;
CREATE UNIQUE INDEX space_invitations_token ON space_invitations (token_hash);
CREATE INDEX space_invitations_by_email ON space_invitations (lower(email))
    WHERE accepted_at IS NULL;

-- Suspending a user blocks sign-in without deleting their files or their
-- authorship of anything.
ALTER TABLE users ADD COLUMN suspended_at TIMESTAMPTZ;
