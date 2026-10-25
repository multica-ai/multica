-- Chat text joins the local search index change log (MUL-7754, migration 561):
-- web and desktop copy the requesting member's chat sessions and their messages
-- and search them alongside issues, comments, and projects.
--
-- chat_message carries no workspace_id, so its trigger resolves the workspace
-- through the session. A session delete cascades to its messages before this
-- AFTER trigger runs, so the lookup finds no session row; that delete needs no
-- message tombstones because the session's own change row (recorded directly
-- from the session row, which does carry workspace_id) makes clients drop the
-- session and every message under it.
--
-- The message trigger stays trivial for the same reason as migration 561's: it
-- runs inside every chat write, so an error here would fail the user's write.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE search_index_change DROP CONSTRAINT search_index_change_entity_type_check;
ALTER TABLE search_index_change ADD CONSTRAINT search_index_change_entity_type_check
    CHECK (entity_type IN ('issue', 'comment', 'project', 'chat_session', 'chat_message'));

CREATE OR REPLACE FUNCTION record_chat_message_search_index_change()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
DECLARE
    changed_id UUID;
    workspace_id UUID;
BEGIN
    IF TG_OP = 'DELETE' THEN
        changed_id := OLD.id;
        workspace_id := (SELECT s.workspace_id FROM chat_session s WHERE s.id = OLD.chat_session_id);
    ELSE
        changed_id := NEW.id;
        workspace_id := (SELECT s.workspace_id FROM chat_session s WHERE s.id = NEW.chat_session_id);
    END IF;

    IF workspace_id IS NULL THEN
        -- Cascaded session delete; the session tombstone covers it.
        RETURN NULL;
    END IF;

    INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_xid, changed_at)
    VALUES (TG_ARGV[0], changed_id, workspace_id, pg_current_xact_id(), now())
    ON CONFLICT (entity_type, entity_id) DO UPDATE
        SET workspace_id = EXCLUDED.workspace_id,
            change_xid = EXCLUDED.change_xid,
            changed_at = EXCLUDED.changed_at;
    RETURN NULL;
END
$function$;

-- Every session column the client indexes (title, status, agent_id) can
-- change, so any update counts, like the issue trigger.
DROP TRIGGER IF EXISTS trg_chat_session_search_index_change ON chat_session;
CREATE TRIGGER trg_chat_session_search_index_change
AFTER INSERT OR UPDATE OR DELETE ON chat_session
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION record_search_index_change('chat_session');

DROP TRIGGER IF EXISTS trg_chat_message_search_index_change ON chat_message;
CREATE TRIGGER trg_chat_message_search_index_change
AFTER INSERT OR UPDATE OR DELETE ON chat_message
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION record_chat_message_search_index_change('chat_message');
