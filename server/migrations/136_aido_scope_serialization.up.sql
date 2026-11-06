-- Serialize both binding and token mint on the same authoritative agent row.
-- Separate PL/pgSQL statements refresh READ COMMITTED visibility after waiting.
CREATE OR REPLACE FUNCTION enforce_aido_owner_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'AIDO binding requires READ COMMITTED visibility';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'AIDO owner binding is immutable';
    END IF;
    PERFORM 1 FROM agent WHERE id=NEW.agent_id AND runtime_id=NEW.runtime_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'AIDO owner runtime must match agent';
    END IF;
    UPDATE task_token SET is_aido_owner=true WHERE agent_id=NEW.agent_id;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION enforce_aido_task_token_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'task token scope requires READ COMMITTED visibility';
    END IF;
    PERFORM 1 FROM agent WHERE id=NEW.agent_id FOR UPDATE;
    IF (TG_OP='UPDATE' AND OLD.is_aido_owner)
       OR EXISTS (SELECT 1 FROM aido_owner_binding WHERE agent_id=NEW.agent_id) THEN
        NEW.is_aido_owner := true;
    END IF;
    RETURN NEW;
END $$;

-- Repair tokens that escaped the former concurrent binding/mint window.
UPDATE task_token SET is_aido_owner=true
WHERE agent_id IN (SELECT agent_id FROM aido_owner_binding);
