ALTER TABLE controlled_run ADD COLUMN request_hash text NOT NULL DEFAULT '';

-- Permanent operation identity survives replacement of the current resource lease.
CREATE TABLE controller_effect_operation (
    workspace_id uuid NOT NULL,
    operation_id text NOT NULL,
    resource_key text NOT NULL,
    issue_id uuid NOT NULL,
    request_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Stable property/status definitions are part of accepted projection authority.
-- Reconfiguration requires a drained controller migration, never a worker edit.
CREATE FUNCTION enforce_controller_definition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='issue_property' THEN
        IF EXISTS(SELECT 1 FROM issue_controller c WHERE c.workspace_id=OLD.workspace_id
          AND c.config->'protected_property_ids' ? OLD.id::text) THEN
            IF TG_OP='DELETE' OR NEW IS DISTINCT FROM OLD THEN
                RAISE EXCEPTION 'property definition is controller owned' USING ERRCODE='42501';
            END IF;
        END IF;
    ELSE
        IF EXISTS(SELECT 1 FROM issue_controller c, jsonb_each_text(c.config->'statuses') s
          WHERE c.workspace_id=OLD.workspace_id AND s.value=OLD.key) THEN
            IF TG_OP='DELETE' OR NEW IS DISTINCT FROM OLD THEN
                RAISE EXCEPTION 'status definition is controller owned' USING ERRCODE='42501';
            END IF;
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER controller_property_definition_guard BEFORE UPDATE OR DELETE ON issue_property
    FOR EACH ROW EXECUTE FUNCTION enforce_controller_definition();
CREATE TRIGGER controller_status_definition_guard BEFORE UPDATE OR DELETE ON issue_status
    FOR EACH ROW EXECUTE FUNCTION enforce_controller_definition();
