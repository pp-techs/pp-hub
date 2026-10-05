-- name: StampCopiedPage :exec
-- A copy is made in one transaction, where now() is the same instant for
-- every row it inserts. Sub-pages are ordered by created_at, then name, so a
-- copied sub-page takes the wall clock instead: copied in their original
-- order, the copies keep it rather than falling back to name order.
UPDATE nodes SET created_at = clock_timestamp() WHERE id = $1;
