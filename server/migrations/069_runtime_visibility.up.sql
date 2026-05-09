ALTER TABLE agent_runtime
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'workspace'
    CHECK (visibility IN ('workspace', 'private'));
