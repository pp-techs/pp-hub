-- Kanban boards. A board is a node (public.nodes, kind 'ext', ext_kind
-- 'kanban.board'); everything here hangs off it and goes with it when the
-- node is purged. Positions are fractional-index strings ordered bytewise
-- (COLLATE "C"), so moving a card or a column rewrites one row.

CREATE TABLE columns (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id   UUID NOT NULL REFERENCES public.nodes(id) ON DELETE CASCADE,
    title      TEXT NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 200),
    position   TEXT COLLATE "C" NOT NULL,
    wip_limit  INTEGER CHECK (wip_limit IS NULL OR wip_limit > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX columns_by_board ON columns (board_id, position, id);

CREATE TABLE labels (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id UUID NOT NULL REFERENCES public.nodes(id) ON DELETE CASCADE,
    name     TEXT NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 60),
    color    TEXT NOT NULL CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
    UNIQUE (board_id, name)
);

CREATE TABLE cards (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id       UUID NOT NULL REFERENCES public.nodes(id) ON DELETE CASCADE,
    column_id      UUID NOT NULL REFERENCES columns(id) ON DELETE CASCADE,
    title          TEXT NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 500),
    description_md TEXT NOT NULL DEFAULT '' CHECK (char_length(description_md) <= 20000),
    position       TEXT COLLATE "C" NOT NULL,
    due_at         TIMESTAMPTZ,
    created_by     UUID REFERENCES public.users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Incremented by every write, so a client holding an older copy is told.
    rev            INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX cards_by_column ON cards (column_id, position, id);
CREATE INDEX cards_by_board ON cards (board_id);

CREATE TABLE card_assignees (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, user_id)
);
-- Prepared for the cross-board "assigned to me" view.
CREATE INDEX card_assignees_by_user ON card_assignees (user_id);

CREATE TABLE card_labels (
    card_id  UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, label_id)
);
