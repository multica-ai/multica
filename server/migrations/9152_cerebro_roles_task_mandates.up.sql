-- FIR-3402: make versioned roles and expiring bindings the only durable
-- access-grant source, and replace the pre-run Agent Pass with a call-time
-- Task Mandate.

ALTER TABLE cerebro_role
    ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS permissions JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE cerebro_role_assignment
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_cerebro_role_assignment_active_subject
    ON cerebro_role_assignment (subject_type, subject_id, expires_at);

CREATE TABLE IF NOT EXISTS cerebro_task_mandate (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL UNIQUE REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    allowed_tools JSONB NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT cerebro_task_mandate_allowed_tools_array
        CHECK (jsonb_typeof(allowed_tools) = 'array'),
    CONSTRAINT cerebro_task_mandate_valid_window
        CHECK (expires_at > issued_at)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_task_mandate_agent_expiry
    ON cerebro_task_mandate (agent_id, expires_at);

-- Preserve current agent-layer choices as versioned, permanent role bindings.
-- Explicitly authored policy remains the source of truth; this migration only
-- packages it into roles so the flip has no decision differences.
INSERT INTO cerebro_role (workspace_id, name, description, version, permissions)
SELECT
    workspace_id,
    'Migrated agent ' || subject_id::text,
    'Automatically migrated from direct agent policy rows',
    1,
    jsonb_object_agg(tool_key, jsonb_build_object(
        'setting', setting,
        'resource_pattern', resource_pattern,
        'conditions', conditions
    ) ORDER BY tool_key)
FROM cerebro_tool_policy
WHERE layer = 'agent'
GROUP BY workspace_id, subject_id
ON CONFLICT (workspace_id, name) DO NOTHING;

INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, expires_at)
SELECT r.id, 'agent', p.subject_id, NULL
FROM (
    SELECT DISTINCT workspace_id, subject_id
    FROM cerebro_tool_policy
    WHERE layer = 'agent'
) p
JOIN cerebro_role r
  ON r.workspace_id = p.workspace_id
 AND r.name = 'Migrated agent ' || p.subject_id::text
ON CONFLICT (role_id, subject_type, subject_id) DO NOTHING;

-- Preserve the two remaining firtal_registry config booleans as explicit,
-- deny-by-default action policies before the config-bearing table is dropped.
INSERT INTO cerebro_tool_policy (
    workspace_id, tool_key, layer, subject_id, setting, resource_pattern, conditions
)
SELECT ag.workspace_id, 'firtal_registry', 'agent', atg.agent_id,
       CASE WHEN COALESCE((atg.config_json ->> 'allowed_apps')::boolean, false)
            THEN 'allow' ELSE 'deny' END,
       'action:list_apps', NULL
FROM agent_tool_grant atg
JOIN agent ag ON ag.id=atg.agent_id
WHERE atg.tool_name='firtal_registry' AND atg.enabled=true
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING;

INSERT INTO cerebro_tool_policy (
    workspace_id, tool_key, layer, subject_id, setting, resource_pattern, conditions
)
SELECT ag.workspace_id, 'firtal_registry', 'agent', atg.agent_id,
       CASE WHEN COALESCE((atg.config_json ->> 'allow_write')::boolean, false)
            THEN 'allow' ELSE 'deny' END,
       'action:update_app', NULL
FROM agent_tool_grant atg
JOIN agent ag ON ag.id=atg.agent_id
WHERE atg.tool_name='firtal_registry' AND atg.enabled=true
ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING;

-- The legacy stores are removed only after their complete state is represented
-- by roles/tool policy. The migration is intentionally irreversible in-place.
DROP TABLE IF EXISTS agent_tool_grant;
DROP TABLE IF EXISTS cerebro_agent_pass CASCADE;
