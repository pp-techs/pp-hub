-- Personal spaces and external sharing.
-- See docs/architecture/personal-spaces.md.
--
-- A personal space is a space no Logto organization mirrors: one per person,
-- its owner its only member, provisioned at sign-in when the instance's
-- `personal_spaces` setting admits them. It is the one kind of space that may
-- stay unlinked, and the check below keeps it that way — org sync must never
-- adopt one.
ALTER TABLE spaces
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'org'
        CONSTRAINT spaces_kind_ck CHECK (kind IN ('org', 'personal')),
    -- Whether content here may be granted to people outside the space. Off on
    -- an organization space until its admins decide otherwise; a personal
    -- space has nobody inside it to share with, so it is created on.
    ADD COLUMN external_sharing BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT spaces_personal_unlinked_ck
        CHECK (kind <> 'personal' OR idp_org_id IS NULL);

CREATE UNIQUE INDEX spaces_personal_owner_uniq ON spaces (owner_id) WHERE kind = 'personal';

-- An external grant reaches a person who is not a member of the node's space.
-- It is what the resolver may honour for a non-member, and nothing else is:
-- no group grant, no ownership. It never re-shares — an outsider cannot see
-- who else is inside, so handing it on is not a decision they can make well.
ALTER TABLE shares
    ADD COLUMN is_external BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT shares_external_direct_ck
        CHECK (NOT is_external OR (kind = 'direct' AND NOT can_share));

-- Who has handed out external access lately: the invitation rate limit.
CREATE INDEX shares_external_by_creator ON shares (created_by, created_at) WHERE is_external;

-- A share waiting for an address with no account yet. Adopted — turned into a
-- shares row and deleted — when somebody signs in with that address verified.
CREATE TABLE pending_shares (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL DEFAULT 'default',
    node_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    email_norm   TEXT NOT NULL CONSTRAINT pending_shares_email_ck CHECK (email_norm = lower(btrim(email_norm)) AND email_norm <> ''),
    level        share_level NOT NULL,
    can_download BOOLEAN NOT NULL,
    created_by   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    CONSTRAINT pending_shares_edit_download_ck CHECK (level <> 'edit' OR can_download),
    CONSTRAINT pending_shares_uniq UNIQUE (node_id, email_norm)
);

CREATE INDEX pending_shares_by_email ON pending_shares (email_norm);
CREATE INDEX pending_shares_by_creator ON pending_shares (created_by, created_at);
