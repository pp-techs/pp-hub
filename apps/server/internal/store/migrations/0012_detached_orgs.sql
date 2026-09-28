-- M38: organizations whose space was deliberately deleted here.
--
-- Deleting a space does not delete its Logto organization — destruction does
-- not travel between the two systems. But the organization's members still
-- carry it in their ID tokens, and the first of them to sign in would have a
-- fresh, empty space created for it. A row here says "this organization had a
-- space and an admin removed it": sign-in and reconcile leave it alone until
-- somebody links a space to it again, which deletes the row.
CREATE TABLE detached_orgs (
    org_id      TEXT PRIMARY KEY,
    detached_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    detached_by UUID REFERENCES users(id) ON DELETE SET NULL
);
