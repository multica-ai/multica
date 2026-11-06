-- Relay authority is assigned when the credential/session is created, never
-- inferred from prompt text or caller-supplied actor headers.
ALTER TABLE personal_access_token ADD COLUMN is_external_relay boolean NOT NULL DEFAULT false;
ALTER TABLE chat_session ADD COLUMN is_external_relay boolean NOT NULL DEFAULT false;
ALTER TABLE agent_task_queue ADD COLUMN execution_policy text NOT NULL DEFAULT 'unrestricted'
    CHECK (execution_policy IN ('unrestricted', 'restricted'));

CREATE FUNCTION enforce_relay_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME IN ('personal_access_token', 'chat_session') THEN
        IF TG_OP = 'UPDATE' AND OLD.is_external_relay IS DISTINCT FROM NEW.is_external_relay THEN
            RAISE EXCEPTION 'relay provenance is immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.execution_policy = 'restricted' THEN
        NEW.execution_policy := 'restricted';
    END IF;
    IF EXISTS (SELECT 1 FROM chat_session WHERE id = NEW.chat_session_id AND is_external_relay)
       OR EXISTS (SELECT 1 FROM agent_task_queue WHERE id = NEW.parent_task_id AND execution_policy = 'restricted') THEN
        NEW.execution_policy := 'restricted';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER relay_token_provenance BEFORE UPDATE ON personal_access_token
    FOR EACH ROW EXECUTE FUNCTION enforce_relay_provenance();
CREATE TRIGGER relay_session_provenance BEFORE UPDATE ON chat_session
    FOR EACH ROW EXECUTE FUNCTION enforce_relay_provenance();
CREATE TRIGGER relay_task_provenance BEFORE INSERT OR UPDATE ON agent_task_queue
    FOR EACH ROW EXECUTE FUNCTION enforce_relay_provenance();
