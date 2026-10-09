-- Page tree order: where a sub-page sits among its siblings.
--
-- A page without a row keeps the tree's default order (created_at, then name)
-- and sorts after every page that has one, so a new sub-page lands at the end
-- and nothing needs backfilling. Moving a page in the tree writes a position
-- for every live sibling under its parent, which makes the new order total.
CREATE TABLE page_order (
    node_id  UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    position INTEGER NOT NULL
);
