-- Operator-controlled, immutable agent/runtime binding. No prompt/header grant.
CREATE TABLE aido_owner_binding (
    agent_id uuid PRIMARY KEY REFERENCES agent(id) ON DELETE CASCADE,
    runtime_id uuid NOT NULL REFERENCES agent_runtime(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE task_token ADD COLUMN is_aido_owner boolean NOT NULL DEFAULT false;

CREATE FUNCTION enforce_aido_owner_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'AIDO owner binding is immutable';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM agent WHERE id=NEW.agent_id AND runtime_id=NEW.runtime_id) THEN
        RAISE EXCEPTION 'AIDO owner runtime must match agent';
    END IF;
    UPDATE task_token SET is_aido_owner=true WHERE agent_id=NEW.agent_id;
    RETURN NEW;
END $$;
CREATE TRIGGER aido_owner_binding_guard BEFORE INSERT OR UPDATE ON aido_owner_binding
FOR EACH ROW EXECUTE FUNCTION enforce_aido_owner_binding();

-- Retire the dedicated agent (and its tokens) as a unit. Deleting just the
-- binding must not make a later token mint silently use broad internal scope.
CREATE FUNCTION preserve_aido_binding_on_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent WHERE id=OLD.agent_id) THEN
        RAISE EXCEPTION 'retire AIDO agent before removing binding';
    END IF;
    RETURN OLD;
END $$;
CREATE TRIGGER aido_binding_delete_guard BEFORE DELETE ON aido_owner_binding
FOR EACH ROW EXECUTE FUNCTION preserve_aido_binding_on_delete();

CREATE FUNCTION enforce_aido_task_token_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (TG_OP='UPDATE' AND OLD.is_aido_owner)
       OR EXISTS (SELECT 1 FROM aido_owner_binding WHERE agent_id=NEW.agent_id) THEN
        NEW.is_aido_owner := true;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER aido_token_scope_guard BEFORE INSERT OR UPDATE ON task_token
FOR EACH ROW EXECUTE FUNCTION enforce_aido_task_token_scope();

CREATE FUNCTION preserve_aido_runtime_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM aido_owner_binding WHERE agent_id=NEW.id AND runtime_id IS DISTINCT FROM NEW.runtime_id) THEN
        RAISE EXCEPTION 'AIDO owner agent cannot move across runtime boundary';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER aido_agent_runtime_guard BEFORE UPDATE OF runtime_id ON agent
FOR EACH ROW EXECUTE FUNCTION preserve_aido_runtime_binding();
