-- Team folders: a folder the *team* owns rather than a person.
--
-- Everything else in this schema hangs off `nodes.created_by`, which is the one
-- unconditional access path: create something and you reach it forever, and so
-- does whoever owns a folder above it. That is right for a member's own top
-- level and wrong for shared company content — it is why revoking a grant
-- cannot take back what the grantee uploaded, why they can always mint a link
-- for it, and why they can move it out from under the folder owner. Google
-- Drive, ownCloud and Nextcloud all reached the same conclusion and grew a
-- second container (shared drive, project space, team folder) where content
-- has no personal owner.
--
-- `team_root_id` marks membership of such a container: NULL for personal
-- content, the container's id for everything inside it, and its own id on the
-- container itself. It is denormalised on purpose — the alternative is an
-- ancestor walk inside every resolution query, and this turns three recursive
-- checks into a column predicate. `fs` maintains it: inherited from the parent
-- on create and copy, rewritten across the subtree on move.
ALTER TABLE nodes ADD COLUMN team_root_id UUID REFERENCES nodes(id) ON DELETE SET NULL;

CREATE INDEX nodes_team_root ON nodes (team_root_id) WHERE team_root_id IS NOT NULL;

-- A team folder is always top level. Nesting one would make "which container
-- owns this" ambiguous, and the whole point is that the answer is never in
-- doubt.
ALTER TABLE nodes ADD CONSTRAINT nodes_team_root_top_level_ck
    CHECK (team_root_id IS DISTINCT FROM id OR parent_id IS NULL);
