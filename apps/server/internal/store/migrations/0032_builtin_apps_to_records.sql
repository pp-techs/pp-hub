-- Kanban and Checklist stop being compiled into the server (M51): they are
-- apps an administrator uploads, like any other, and keep their data in the
-- record store. This moves what the built-ins kept in their own schemas into
-- ext_records, in the shapes the uploaded apps read (examples/apps/kanban,
-- examples/apps/checklist), and drops those schemas and the per-extension
-- migration track nothing writes any more.
--
-- Ids are kept: a card's id becomes its record's, so links, comment threads
-- and notifications that name a card keep naming it. A board or checklist
-- opens as "not part of this installation" until the app is uploaded again,
-- and then opens whole.
--
-- The schemas exist only where the built-ins ever ran, hence the guards.

DO $$
BEGIN
    IF to_regclass('ext_kanban.cards') IS NOT NULL THEN
        INSERT INTO ext_records (node_id, id, collection, data, rev, created_at, updated_at)
        SELECT c.board_id, c.id, 'columns',
               jsonb_build_object('title', c.title, 'position', c.position, 'wip_limit', c.wip_limit),
               1, c.created_at, c.created_at
        FROM ext_kanban.columns c;

        INSERT INTO ext_records (node_id, id, collection, data)
        SELECT l.board_id, l.id, 'labels', jsonb_build_object('name', l.name, 'color', l.color)
        FROM ext_kanban.labels l;

        INSERT INTO ext_records (node_id, id, collection, data, rev, created_by, updated_by, created_at, updated_at)
        SELECT c.board_id, c.id, 'cards',
               jsonb_build_object(
                   'column_id', c.column_id,
                   'title', c.title,
                   'description_md', c.description_md,
                   'position', c.position,
                   'due_at', to_char(c.due_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                   'assignees', COALESCE((SELECT jsonb_agg(a.user_id ORDER BY a.user_id)
                                            FROM ext_kanban.card_assignees a WHERE a.card_id = c.id), '[]'::jsonb),
                   'label_ids', COALESCE((SELECT jsonb_agg(cl.label_id ORDER BY cl.label_id)
                                            FROM ext_kanban.card_labels cl WHERE cl.card_id = c.id), '[]'::jsonb),
                   'link_ids', COALESCE((SELECT jsonb_agg(k.node_id ORDER BY k.node_id)
                                           FROM ext_kanban.card_links k WHERE k.card_id = c.id), '[]'::jsonb)),
               c.rev, c.created_by, c.created_by, c.created_at, c.updated_at
        FROM ext_kanban.cards c;

        -- A card's discussion and notices named it "card:<id>"; a record is
        -- "record:<id>".
        UPDATE comment_threads t SET subject = 'record:' || substr(t.subject, 6)
        FROM nodes n
        WHERE n.id = t.node_id AND n.ext_kind = 'kanban.board' AND t.subject LIKE 'card:%';
        UPDATE notifications o SET subject = 'record:' || substr(o.subject, 6)
        FROM nodes n
        WHERE n.id = o.node_id AND n.ext_kind = 'kanban.board' AND o.subject LIKE 'card:%';
    END IF;

    IF to_regclass('ext_checklist.items') IS NOT NULL THEN
        -- Records list in (created_at, id) order and items were kept in seq
        -- order; items made in one transaction share a timestamp, so each
        -- gets a microsecond more than the one before it.
        INSERT INTO ext_records (node_id, id, collection, data, created_by, updated_by, created_at, updated_at)
        SELECT o.list_id, o.id, 'items', jsonb_build_object('text', o.text, 'done', o.done),
               o.created_by, o.created_by, o.at, o.at
        FROM (
            SELECT i.list_id, i.id, i.text, i.done, i.created_by,
                   max(i.created_at) OVER w
                       + (row_number() OVER w) * interval '1 microsecond' AS at
            FROM ext_checklist.items i
            WINDOW w AS (PARTITION BY i.list_id ORDER BY i.seq ROWS UNBOUNDED PRECEDING)
        ) o;
    END IF;
END $$;

DROP SCHEMA IF EXISTS ext_kanban CASCADE;
DROP SCHEMA IF EXISTS ext_checklist CASCADE;
DROP TABLE IF EXISTS ext_migrations;
