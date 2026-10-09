-- Persist the queue's thread scope so uniqueness remains atomic for every
-- writer, including retries and older clients. This is derived data, not a FK.
ALTER TABLE agent_task_queue ADD COLUMN comment_thread_id uuid;

CREATE FUNCTION comment_thread_root_id(comment_id uuid) RETURNS uuid
LANGUAGE sql STABLE STRICT AS $$
    WITH RECURSIVE ancestors AS (
        SELECT c.id, c.parent_id, c.issue_id, ARRAY[c.id] AS path
        FROM comment c WHERE c.id = comment_id
        UNION ALL
        SELECT p.id, p.parent_id, p.issue_id, a.path || p.id
        FROM ancestors a JOIN comment p ON p.id = a.parent_id AND p.issue_id = a.issue_id
        WHERE NOT p.id = ANY(a.path)
    )
    SELECT COALESCE(
        (SELECT id FROM ancestors ORDER BY cardinality(path) DESC LIMIT 1),
        comment_id
    )
$$;

CREATE FUNCTION set_agent_task_comment_thread() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.comment_thread_id := comment_thread_root_id(NEW.trigger_comment_id);
    RETURN NEW;
END
$$;

CREATE TRIGGER agent_task_comment_thread
BEFORE INSERT OR UPDATE OF trigger_comment_id ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION set_agent_task_comment_thread();

-- Existing rows intentionally retain a NULL thread scope. Pre-migration tasks
-- drain under the issue/agent claim fence without rewriting historical data.

-- Do NOT add a backfill here. Migration 451 originally shipped one and it was
-- removed: rewriting the whole historical queue at startup is unsafe against
-- production data volume, and there is nothing to gain — the only rows without
-- a thread scope are the tasks in flight during the rolling deploy, and their
-- historical thread scope has no value. #8229 proposed doing it again as
-- migration 457 and was closed for the same reason.
