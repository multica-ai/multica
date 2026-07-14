ALTER TABLE lark_installation
    ADD COLUMN runtime_id UUID REFERENCES agent_runtime(id) ON DELETE SET NULL;

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_kind_target_check;

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_kind_target_check
    CHECK (
        (installation_kind = 'agent' AND agent_id IS NOT NULL)
        OR (installation_kind = 'notification' AND agent_id IS NULL AND runtime_id IS NULL)
    );

CREATE INDEX idx_lark_installation_runtime
    ON lark_installation(runtime_id)
    WHERE runtime_id IS NOT NULL;
