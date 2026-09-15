-- M7: Search. Trigram index so an infix match on a file name stays indexed.
--
-- A plain B-tree on nodes.name only answers prefix queries, and people search
-- for "forecast" expecting to find "Q1-forecast-final.xlsx". A GIN trigram
-- index answers ILIKE '%…%' directly, and the same index backs similarity()
-- for ranking.
--
-- The index is partial on deleted_at IS NULL to match every search query and
-- to keep trashed rows out of the index entirely.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX nodes_name_trgm ON nodes USING gin (name gin_trgm_ops)
    WHERE deleted_at IS NULL;

-- Search always filters by the caller's space membership before it filters by
-- name, so membership lookup by user must be cheap. The primary key on
-- space_members is (space_id, user_id), which cannot serve a user-first probe.
CREATE INDEX space_members_by_user ON space_members (user_id);
