-- A checklist is a node; its items are the rows below. Purging the node takes
-- them with it.
CREATE TABLE items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    list_id    UUID NOT NULL REFERENCES public.nodes(id) ON DELETE CASCADE,
    -- Order of creation; an item keeps its place when it is ticked.
    seq        BIGINT GENERATED ALWAYS AS IDENTITY,
    text       TEXT NOT NULL CHECK (btrim(text) <> '' AND char_length(text) <= 500),
    done       BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES public.users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX items_by_list ON items (list_id, seq);
