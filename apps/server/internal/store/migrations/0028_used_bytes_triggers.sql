-- spaces.used_bytes maintained by the database.
--
-- Every write used to re-sum the whole space (SumSpaceUsage) and store the
-- result, and a purge never recounted at all, so the meter ran high. Every
-- writer already goes through node_versions / node_attachments and
-- nodes.space_id never changes, so row triggers keep the figure exact.
--
-- Version and attachment rows move used_bytes by their size, so no writer
-- re-sums the space. A purge is subtracted once, by the node being deleted
-- (BEFORE DELETE still sees its versions); the cascaded version/attachment
-- deletes that follow find their node gone and skip, so nothing counts twice.
CREATE FUNCTION space_usage_add() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE spaces SET used_bytes = used_bytes + NEW.size
     WHERE id = (SELECT space_id FROM nodes WHERE id = NEW.node_id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION space_usage_remove() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sid UUID;
BEGIN
    SELECT space_id INTO sid FROM nodes WHERE id = OLD.node_id;
    IF FOUND THEN
        UPDATE spaces SET used_bytes = GREATEST(used_bytes - OLD.size, 0) WHERE id = sid;
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION space_usage_node_purge() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE spaces
       SET used_bytes = GREATEST(used_bytes
           - (SELECT COALESCE(SUM(size), 0) FROM node_versions WHERE node_id = OLD.id)
           - (SELECT COALESCE(SUM(size), 0) FROM node_attachments WHERE node_id = OLD.id), 0)
     WHERE id = OLD.space_id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER node_versions_usage_ins AFTER INSERT ON node_versions FOR EACH ROW EXECUTE FUNCTION space_usage_add();
CREATE TRIGGER node_versions_usage_del AFTER DELETE ON node_versions FOR EACH ROW EXECUTE FUNCTION space_usage_remove();
CREATE TRIGGER node_attachments_usage_ins AFTER INSERT ON node_attachments FOR EACH ROW EXECUTE FUNCTION space_usage_add();
CREATE TRIGGER node_attachments_usage_del AFTER DELETE ON node_attachments FOR EACH ROW EXECUTE FUNCTION space_usage_remove();
CREATE TRIGGER nodes_usage_purge BEFORE DELETE ON nodes FOR EACH ROW EXECUTE FUNCTION space_usage_node_purge();

-- Start from true counts: purges never recounted, so stored figures ran high.
UPDATE spaces s SET used_bytes =
    COALESCE((SELECT SUM(v.size) FROM node_versions v JOIN nodes n ON n.id = v.node_id WHERE n.space_id = s.id), 0)
  + COALESCE((SELECT SUM(a.size) FROM node_attachments a JOIN nodes n ON n.id = a.node_id WHERE n.space_id = s.id), 0);
