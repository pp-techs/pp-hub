-- M43: pages — sub-pages and page links between block documents.
--
-- A sub-page is a `.md` node whose parent_id is its parent document; the
-- tree itself needs no column. These two tables are projections of a
-- document's current version for batch reads, written by the markdown
-- `extract:text` job (and its startup backfill), guarded like `node_texts`
-- so a late job for a superseded version writes nothing.

-- The page's icon and cover, from its frontmatter (`icon`, `cover`). NULL is
-- none. node_version_id is the version they were read from.
CREATE TABLE page_meta (
    node_id         UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    node_version_id UUID NOT NULL,
    icon            TEXT,
    cover           TEXT
);

-- Every page a document's current version names: its sub-page blocks
-- ('sub') and its inline page links ('link'). target_id has no foreign key:
-- a link may name a page that is gone, or was never there, and the row must
-- not stop that page's purge. Backlinks read it by target.
CREATE TABLE page_refs (
    source_id       UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id       UUID NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('sub', 'link')),
    node_version_id UUID NOT NULL,
    PRIMARY KEY (source_id, target_id, kind)
);

CREATE INDEX page_refs_target ON page_refs (target_id);
