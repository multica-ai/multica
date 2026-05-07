-- CEREBRO-PATCH(migration-idempotent-004-agent-runtime-loop): cerebro modification of upstream file
CREATE TABLE IF NOT EXISTS agent_runtime (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    daemon_id TEXT,
    name TEXT NOT NULL,
    runtime_mode TEXT NOT NULL CHECK (runtime_mode IN ('local', 'cloud')),
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('online', 'offline')),
    device_info TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, daemon_id, provider)
);

ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS runtime_id UUID;

INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    last_seen_at,
    created_at,
    updated_at
)
SELECT
    a.workspace_id,
    NULL,
    COALESCE(NULLIF(a.runtime_config->>'runtime_name', ''), a.name || ' Runtime'),
    a.runtime_mode,
    COALESCE(
        NULLIF(a.runtime_config->>'provider', ''),
        CASE
            WHEN a.runtime_mode = 'cloud' THEN 'multica_agent'
            ELSE 'legacy_local'
        END
    ),
    CASE
        WHEN a.status = 'offline' THEN 'offline'
        ELSE 'online'
    END,
    COALESCE(
        NULLIF(a.runtime_config->>'runtime_name', ''),
        CASE
            WHEN a.runtime_mode = 'cloud' THEN 'Cloud runtime'
            ELSE 'Local runtime'
        END
    ),
    jsonb_build_object('migrated_agent_id', a.id::text),
    CASE
        WHEN a.status = 'offline' THEN NULL
        ELSE a.updated_at
    END,
    a.created_at,
    a.updated_at
FROM agent a
WHERE NOT EXISTS (
    SELECT 1 FROM agent_runtime ar
    WHERE ar.metadata->>'migrated_agent_id' = a.id::text
);

UPDATE agent a
SET runtime_id = ar.id
FROM agent_runtime ar
WHERE ar.metadata->>'migrated_agent_id' = a.id::text
  AND a.runtime_id IS DISTINCT FROM ar.id;

ALTER TABLE agent
    ALTER COLUMN runtime_id SET NOT NULL;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'agent_runtime_id_fkey') THEN
        ALTER TABLE agent
            ADD CONSTRAINT agent_runtime_id_fkey
                FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE RESTRICT;
    END IF;
END $$;

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS runtime_id UUID;

UPDATE agent_task_queue atq
SET runtime_id = a.runtime_id
FROM agent a
WHERE a.id = atq.agent_id
  AND atq.runtime_id IS DISTINCT FROM a.runtime_id;

ALTER TABLE agent_task_queue
    ALTER COLUMN runtime_id SET NOT NULL;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'agent_task_queue_runtime_id_fkey') THEN
        ALTER TABLE agent_task_queue
            ADD CONSTRAINT agent_task_queue_runtime_id_fkey
                FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_agent_runtime_workspace ON agent_runtime(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_runtime_status ON agent_runtime(workspace_id, status);
CREATE INDEX IF NOT EXISTS idx_agent_task_queue_runtime_pending
    ON agent_task_queue(runtime_id, priority DESC, created_at ASC)
    WHERE status IN ('queued', 'dispatched');
