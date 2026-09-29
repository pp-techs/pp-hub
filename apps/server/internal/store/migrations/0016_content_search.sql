-- M27: content search for markdown and plain-text files.
--
-- One row per file: the text extracted from its *current* version. The
-- `extract:text` job upserts it whenever a version is committed (or an older
-- one restored), guarded so a late job for a superseded version can never
-- overwrite the newer text. Search still joins on node_version_id =
-- nodes.current_version_id, so a row that has not caught up yet is simply not
-- matched rather than served stale.
--
-- Purge deletes the node and the row goes with it; version pruning cannot
-- strand one either, since a node's current version is never pruned.
CREATE TABLE node_texts (
    node_id         UUID PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    node_version_id UUID NOT NULL REFERENCES node_versions(id) ON DELETE CASCADE,
    -- What snippets are cut from: NFC-normalised, capped, valid UTF-8.
    content         TEXT NOT NULL,
    -- `content` lower-cased with diacritics removed, one character per
    -- character of `content` (textsearch.Fold), so a position found here is
    -- the same position there. Vietnamese is searched through this column:
    -- the `simple` configuration does no stemming, and folding in Go keeps the
    -- index independent of the unaccent extension and the database locale.
    folded          TEXT NOT NULL,
    tsv             TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', folded)) STORED,
    extracted_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX node_texts_tsv ON node_texts USING gin (tsv);
-- Substring fallback (mid-word hits, punctuation, scripts the text-search
-- parser does not split) runs LIKE against `folded`.
CREATE INDEX node_texts_folded_trgm ON node_texts USING gin (folded gin_trgm_ops);
CREATE INDEX node_texts_version ON node_texts (node_version_id);
