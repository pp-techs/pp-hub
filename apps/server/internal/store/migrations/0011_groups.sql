-- M11: Groups and folder grants. See docs/architecture/permissions.md.
--
-- A space is a company. Inside it, access is granted group -> folder rather
-- than by a space-wide role, so the role scale collapses to admin | member and
-- a plain member starts with NO implicit access at all (rank 0). Everything a
-- member can reach traces back to one row in `shares`.
--
-- Enum changes swap in a new type rather than using ALTER TYPE ... ADD VALUE,
-- because a value added that way cannot be used in the same transaction -- and
-- the migration runner wraps this file in exactly one.
--
-- ORDER MATTERS: the grant backfill runs BEFORE the role collapse, because
-- afterwards `editor` and `viewer` are both `member` and no longer tell apart.

-- ---------------------------------------------------------------- groups ---

CREATE TABLE groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    space_id    UUID NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- The auto group: every member of the space belongs to it. Membership is
    -- maintained by the server on join/leave, never edited by hand, and the
    -- group cannot be deleted -- it is how a new hire gets any access at all.
    is_everyone BOOLEAN NOT NULL DEFAULT false,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX groups_name_uniq     ON groups (space_id, lower(name));
CREATE UNIQUE INDEX groups_everyone_uniq ON groups (space_id) WHERE is_everyone;

CREATE TABLE group_members (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

-- The primary key cannot serve a user-first probe, and permission resolution
-- always asks "which groups is this user in?" -- the same reason
-- space_members_by_user exists (0007_search.sql).
CREATE INDEX group_members_by_user ON group_members (user_id);

-- ------------------------------------------------------ shares get groups ---

-- The unnamed table CHECK from 0003_shares.sql (which Postgres named
-- shares_check) has to go FIRST: it compares kind against share_kind literals,
-- so re-validating it during the column type swap fails with
-- "operator does not exist: share_kind_v2 = share_kind".
-- Dropped by literal name deliberately: a DO block that discovered the name
-- would be opaque to sqlc, which parses these files as its schema.
ALTER TABLE shares DROP CONSTRAINT shares_check;

CREATE TYPE share_kind_v2 AS ENUM ('link', 'direct', 'group');
ALTER TABLE shares ALTER COLUMN kind TYPE share_kind_v2
    USING kind::text::share_kind_v2;
DROP TYPE share_kind;
ALTER TYPE share_kind_v2 RENAME TO share_kind;

ALTER TABLE shares ADD COLUMN grantee_group_id UUID REFERENCES groups(id) ON DELETE CASCADE;

ALTER TABLE shares ADD CONSTRAINT shares_grantee_ck CHECK (
    (kind = 'link'   AND token_hash IS NOT NULL AND grantee_user_id IS NULL AND grantee_group_id IS NULL) OR
    (kind = 'direct' AND grantee_user_id  IS NOT NULL AND grantee_group_id IS NULL) OR
    (kind = 'group'  AND grantee_group_id IS NOT NULL AND grantee_user_id  IS NULL)
);

-- Re-granting the same group on the same folder updates the row rather than
-- stacking a second grant that would then have to be resolved against the first.
CREATE UNIQUE INDEX shares_group_uniq ON shares (node_id, grantee_group_id) WHERE kind = 'group';
CREATE INDEX shares_by_group ON shares (grantee_group_id, node_id) WHERE kind = 'group';

-- -------------------------------------------------------------- backfill ---
-- This is what makes the role collapse safe: afterwards every user's effective
-- permission on every node is unchanged. Admins tighten from here.
--
-- TWO groups are created, not one. A single "Everyone" group carrying an edit
-- grant would hand former *viewers* edit -- an escalation, not a migration. So
-- Everyone carries `view` (exactly what a viewer had) and a second, deletable
-- group carries `edit` for the former editors.

-- 1. The auto group, holding every non-owner member.
INSERT INTO groups (space_id, name, description, is_everyone, created_by)
SELECT s.id, 'Everyone',
       'Everyone in this space. Membership is maintained automatically.',
       true, s.owner_id
FROM spaces s;

INSERT INTO group_members (group_id, user_id)
SELECT g.id, m.user_id
FROM groups g
JOIN space_members m ON m.space_id = g.space_id
WHERE g.is_everyone AND m.role <> 'owner';

-- 2. The former editors, so they keep write access. Only for spaces that have
--    one, so a space with no editors is not left with an empty stub group.
INSERT INTO groups (space_id, name, description, is_everyone, created_by)
SELECT s.id, 'Editors (migrated)',
       'Members who held the old editor role. Safe to delete once real groups are set up.',
       false, s.owner_id
FROM spaces s
WHERE EXISTS (
    SELECT 1 FROM space_members m WHERE m.space_id = s.id AND m.role = 'editor'
);

INSERT INTO group_members (group_id, user_id)
SELECT g.id, m.user_id
FROM groups g
JOIN space_members m ON m.space_id = g.space_id
WHERE g.name = 'Editors (migrated)' AND m.role = 'editor';

-- 3. Grants on every root node. Tombstoned roots are included on purpose: a
--    restore has to bring its access back with it.
INSERT INTO shares (node_id, kind, perm, grantee_group_id, created_by)
SELECT n.id, 'group', 'view', g.id, s.owner_id
FROM nodes n
JOIN spaces s ON s.id = n.space_id
JOIN groups g ON g.space_id = n.space_id AND g.is_everyone
WHERE n.parent_id IS NULL;

INSERT INTO shares (node_id, kind, perm, grantee_group_id, created_by)
SELECT n.id, 'group', 'edit', g.id, s.owner_id
FROM nodes n
JOIN spaces s ON s.id = n.space_id
JOIN groups g ON g.space_id = n.space_id AND g.name = 'Editors (migrated)'
WHERE n.parent_id IS NULL;

-- ------------------------------------------------------------ role scale ---
-- owner -> admin. editor AND viewer -> member: both lose implicit access, which
-- the grants above have already handed back explicitly.

CREATE TYPE member_role_v2 AS ENUM ('admin', 'member');

-- Defaults are typed against the old enum and must go first.
ALTER TABLE spaces            ALTER COLUMN default_role DROP DEFAULT;
ALTER TABLE space_invitations ALTER COLUMN role         DROP DEFAULT;

ALTER TABLE space_members ALTER COLUMN role TYPE member_role_v2
    USING (CASE WHEN role = 'owner' THEN 'admin' ELSE 'member' END)::member_role_v2;
ALTER TABLE space_invitations ALTER COLUMN role TYPE member_role_v2
    USING (CASE WHEN role = 'owner' THEN 'admin' ELSE 'member' END)::member_role_v2;
ALTER TABLE spaces ALTER COLUMN default_role TYPE member_role_v2
    USING (CASE WHEN default_role = 'owner' THEN 'admin' ELSE 'member' END)::member_role_v2;

ALTER TABLE spaces            ALTER COLUMN default_role SET DEFAULT 'member';
ALTER TABLE space_invitations ALTER COLUMN role         SET DEFAULT 'member';

DROP TYPE member_role;
ALTER TYPE member_role_v2 RENAME TO member_role;

-- Where per-member personal folders live (populated in 0012).
ALTER TABLE spaces ADD COLUMN personal_root_id UUID REFERENCES nodes(id) ON DELETE SET NULL;
