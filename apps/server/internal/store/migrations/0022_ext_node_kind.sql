-- M44: extensions. An extension node is a node whose content is the rows its
-- extension keeps, not a file: it has no version and no blob.
--
-- The enum value gets a migration of its own because Postgres refuses to use
-- a value in the transaction that adds it, and 0023 constrains on it.
ALTER TYPE node_kind ADD VALUE IF NOT EXISTS 'ext';
