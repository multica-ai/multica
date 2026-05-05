-- CEREBRO-PATCH(migration-idempotent-032-runtime-owner): cerebro modification of upstream file
ALTER TABLE agent_runtime ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES "user"(id);

-- Backfill: set existing runtimes' owner to the workspace owner
UPDATE agent_runtime ar
SET owner_id = (
    SELECT m.user_id FROM member m
    WHERE m.workspace_id = ar.workspace_id AND m.role = 'owner'
    LIMIT 1
);
