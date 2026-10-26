-- Do not remove quarantine while external sessions/tasks still exist.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM personal_access_token WHERE is_external_relay)
       OR EXISTS (SELECT 1 FROM chat_session WHERE is_external_relay)
       OR EXISTS (SELECT 1 FROM agent_task_queue WHERE execution_policy = 'restricted') THEN
        RAISE EXCEPTION 'reconcile quarantined relay data before rollback';
    END IF;
END $$;
DROP TRIGGER relay_token_provenance ON personal_access_token;
DROP TRIGGER relay_session_provenance ON chat_session;
DROP TRIGGER relay_task_provenance ON agent_task_queue;
DROP FUNCTION enforce_relay_provenance();
ALTER TABLE agent_task_queue DROP COLUMN execution_policy;
ALTER TABLE chat_session DROP COLUMN is_external_relay;
ALTER TABLE personal_access_token DROP COLUMN is_external_relay;
