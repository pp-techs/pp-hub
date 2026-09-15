-- M12: Personal folders.
--
-- Every member gets a folder of their own in each space they belong to. Without
-- one a new member signs in to a drive they cannot write a single byte into
-- until an admin grants them something, which is a poor first minute.
--
-- Placement: nodes_name_uniq is on (space_id, COALESCE(parent_id, ...), name),
-- so a folder named after each person at the top level would collide the moment
-- two people share a display name, and would bury the roots that matter under a
-- list of staff. Instead each space gets ONE "Personal" folder, recorded in
-- spaces.personal_root_id, and each member gets a subfolder inside it.
--
-- The grant is an ordinary direct share. Nothing about a personal folder is a
-- special case in permission resolution — it shows up in the grants list like
-- anything else, and an admin can hand it to someone else when a person leaves.

-- One "Personal" folder per space, created by the space owner.
WITH created AS (
    INSERT INTO nodes (space_id, parent_id, kind, name, created_by)
    SELECT s.id, NULL, 'dir', 'Personal', s.owner_id
    FROM spaces s
    WHERE s.personal_root_id IS NULL
      -- A space that already has a live root called Personal keeps it rather
      -- than ending up with "Personal" and "Personal (2)".
      AND NOT EXISTS (
        SELECT 1 FROM nodes n
        WHERE n.space_id = s.id AND n.parent_id IS NULL
          AND n.name = 'Personal' AND n.deleted_at IS NULL
      )
    RETURNING id, space_id
)
UPDATE spaces s SET personal_root_id = c.id
FROM created c WHERE c.space_id = s.id;

-- Adopt a pre-existing "Personal" root where one was in the way above.
UPDATE spaces s SET personal_root_id = n.id
FROM nodes n
WHERE s.personal_root_id IS NULL
  AND n.space_id = s.id AND n.parent_id IS NULL
  AND n.name = 'Personal' AND n.deleted_at IS NULL;

-- A subfolder per member, named for the person: display name, then name, then
-- the local part of their email — whichever is first non-empty, so the folder
-- is recognisable however sparse the account is.
--
-- Two colleagues can share a display name, and nodes_name_uniq would reject the
-- second. row_number() disambiguates them as "Name (2)", "Name (3)" — the same
-- convention fs.Restore uses via suffixName, rather than stamping a uuid
-- fragment on a folder a person has to look at every day.
WITH candidates AS (
    SELECT m.user_id,
           s.id AS space_id,
           s.personal_root_id,
           COALESCE(
             NULLIF(u.display_name, ''),
             NULLIF(u.name, ''),
             NULLIF(split_part(u.email, '@', 1), ''),
             'Member'
           ) AS base
    FROM space_members m
    JOIN spaces s ON s.id = m.space_id
    JOIN users u ON u.id = m.user_id
    WHERE s.personal_root_id IS NOT NULL
),
numbered AS (
    SELECT c.*,
           row_number() OVER (PARTITION BY c.space_id, lower(c.base) ORDER BY c.user_id) AS rn
    FROM candidates c
),
mine AS (
    INSERT INTO nodes (space_id, parent_id, kind, name, created_by)
    SELECT n.space_id, n.personal_root_id, 'dir',
           CASE WHEN n.rn = 1 THEN n.base ELSE n.base || ' (' || n.rn || ')' END,
           n.user_id
    FROM numbered n
    RETURNING id, created_by
)
INSERT INTO shares (node_id, kind, perm, grantee_user_id, created_by)
SELECT mine.id, 'direct', 'edit', mine.created_by, mine.created_by FROM mine;
