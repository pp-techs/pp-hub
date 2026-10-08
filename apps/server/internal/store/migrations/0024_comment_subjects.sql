-- M46: discussion on the parts of an extension node (a kanban card).
--
-- A markdown document anchors its threads in its own text, so a thread there
-- has no subject. A thread on an extension node names the part it is about
-- ("card:<uuid>"), which the extension validates when the thread is opened.
-- Threads still cascade with the node.
ALTER TABLE comment_threads ADD COLUMN subject TEXT
    CHECK (subject IS NULL OR char_length(subject) <= 200);
CREATE INDEX comment_threads_by_subject ON comment_threads (node_id, subject)
    WHERE subject IS NOT NULL;

-- What a notification points at inside its node, for a notification that has
-- no thread (an assignment). A comment's notification reads its thread's.
ALTER TABLE notifications ADD COLUMN subject TEXT;
