DROP INDEX IF EXISTS idx_lark_installation_runtime;

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_kind_target_check;

ALTER TABLE lark_installation
    DROP COLUMN IF EXISTS runtime_id;

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_kind_target_check
    CHECK (
        (installation_kind = 'agent' AND agent_id IS NOT NULL)
        OR (installation_kind = 'notification' AND agent_id IS NULL)
    );
