-- M29: comments on block documents, and in-app notifications.
--
-- A thread belongs to a document and lives exactly as long as it does: the
-- rows cascade when the node is purged (a trashed document keeps its threads,
-- so restoring it restores the conversation). The markdown carries only the
-- thread's id, in the comment marker around the text it annotates, so a
-- thread survives a room reload because its anchor is in the file, not here.
CREATE TABLE comment_threads (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id     UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by UUID REFERENCES users(id),
    CONSTRAINT comment_threads_resolution CHECK ((resolved_at IS NULL) = (resolved_by IS NULL))
);

CREATE INDEX comment_threads_node ON comment_threads (node_id, created_at);

-- A thread with no comment left is deleted with its last one, by the service,
-- in the same transaction.
CREATE TABLE comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id  UUID NOT NULL REFERENCES comment_threads(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES users(id),
    body       TEXT NOT NULL CHECK (body <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at  TIMESTAMPTZ
);

CREATE INDEX comments_thread ON comments (thread_id, created_at);

-- One row per person told about something. A comment's rows are written in
-- the comment's own transaction; a document mention's by the notify:mentions
-- job, which records the version that added it (version_id) so a retried job
-- cannot tell anybody twice. version_id is not a foreign key: pruning the
-- version must not take the notification with it.
--
-- Read rows are purged after a retention window by the server's sweep
-- (notifications.ReadRetention); unread ones stay until they are read, or
-- until their document, thread or comment is deleted.
CREATE TABLE notifications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('comment', 'reply', 'mention')),
    actor_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    thread_id  UUID REFERENCES comment_threads(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comments(id) ON DELETE CASCADE,
    version_id UUID,
    excerpt    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at    TIMESTAMPTZ
);

CREATE INDEX notifications_user_recent ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_user_unread ON notifications (user_id) WHERE read_at IS NULL;
CREATE INDEX notifications_read_at ON notifications (read_at) WHERE read_at IS NOT NULL;
CREATE UNIQUE INDEX notifications_version_once ON notifications (user_id, version_id)
    WHERE version_id IS NOT NULL;
