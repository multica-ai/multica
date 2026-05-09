CREATE TABLE agent_runtime (
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
    ADD COLUMN runtime_id UUID;

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
FROM agent a;

UPDATE agent a
SET runtime_id = ar.id
FROM agent_runtime ar
WHERE ar.metadata->>'migrated_agent_id' = a.id::text;

ALTER TABLE agent
    ALTER COLUMN runtime_id SET NOT NULL,
    ADD CONSTRAINT agent_runtime_id_fkey
        FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE RESTRICT;

ALTER TABLE agent_task_queue
    ADD COLUMN runtime_id UUID;

UPDATE agent_task_queue atq
SET runtime_id = a.runtime_id
FROM agent a
WHERE a.id = atq.agent_id;

ALTER TABLE agent_task_queue
    ALTER COLUMN runtime_id SET NOT NULL,
    ADD CONSTRAINT agent_task_queue_runtime_id_fkey
        FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE CASCADE;

CREATE INDEX idx_agent_runtime_workspace ON agent_runtime(workspace_id);
CREATE INDEX idx_agent_runtime_status ON agent_runtime(workspace_id, status);
CREATE INDEX idx_agent_task_queue_runtime_pending
    ON agent_task_queue(runtime_id, priority DESC, created_at ASC)
    WHERE status IN ('queued', 'dispatched');
