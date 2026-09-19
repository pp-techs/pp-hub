-- A node parented to itself makes every ancestor-walking CTE loop forever —
-- NodeAccessCaps uses UNION ALL, so capability resolution for anything beneath
-- the cycle never returns and the request holds its connection until the
-- client gives up. RemoveMember created exactly that shape: the space creator
-- owns the Archive folder, so ListArchiveRoots handed it back as one of "their
-- roots" and MoveUnderArchiveRoot parented it to itself.
--
-- Repair first, then make the shape unrepresentable.
UPDATE nodes SET parent_id = NULL WHERE id = parent_id;

ALTER TABLE nodes ADD CONSTRAINT nodes_no_self_parent_ck
    CHECK (parent_id IS DISTINCT FROM id);
