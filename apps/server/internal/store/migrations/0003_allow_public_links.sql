-- Ownership is intrinsic, so whoever creates a node can always share it —
-- including as a public link — no matter what the grant on the folder above
-- said about re-sharing. That is the right default for a personal top level
-- and the wrong one for a company that does not publish anything, so the
-- switch lives where the policy does: on the space.
ALTER TABLE spaces ADD COLUMN allow_public_links BOOLEAN NOT NULL DEFAULT TRUE;
