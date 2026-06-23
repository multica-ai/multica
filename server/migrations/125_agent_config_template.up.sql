-- Agent config templates: system-level and personal-level configuration
-- templates that can be bound to agents for layered config merge.

CREATE TABLE agent_config_template (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    scope VARCHAR(20) NOT NULL CHECK (scope IN ('system', 'personal')),
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}',
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_by UUID REFERENCES member(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_config_template_ws_scope ON agent_config_template(workspace_id, scope);

-- Each workspace can have at most one system default template
CREATE UNIQUE INDEX idx_act_default_system ON agent_config_template(workspace_id)
    WHERE scope = 'system' AND is_default = true;

-- Each user can have at most one personal default template per workspace
CREATE UNIQUE INDEX idx_act_default_personal ON agent_config_template(workspace_id, created_by)
    WHERE scope = 'personal' AND is_default = true;

-- Agent table: add template binding columns
ALTER TABLE agent ADD COLUMN system_template_id UUID REFERENCES agent_config_template(id) ON DELETE SET NULL;
ALTER TABLE agent ADD COLUMN personal_template_id UUID REFERENCES agent_config_template(id) ON DELETE SET NULL;

-- Skip flags: when true, the agent skips that template layer entirely
-- (no template, no default, no legacy fallback). Useful for agents that
-- want full manual control over their configuration.
ALTER TABLE agent ADD COLUMN skip_system_template BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agent ADD COLUMN skip_personal_template BOOLEAN NOT NULL DEFAULT false;

-- Migrate existing workspace.agent_defaults → system default templates
INSERT INTO agent_config_template (workspace_id, scope, name, description, config, is_default, created_at, updated_at)
SELECT
    w.id,
    'system',
    '系统Default',
    '从workspace settings自动迁移的系统默认配置',
    COALESCE((w.settings->'agent_defaults')::jsonb, '{}'::jsonb),
    true,
    now(),
    now()
FROM workspace w
WHERE w.settings ? 'agent_defaults'
  AND w.settings->'agent_defaults' IS NOT NULL
  AND w.settings->'agent_defaults' != 'null'::jsonb;

-- Migrate existing member_agent_config → personal default templates
INSERT INTO agent_config_template (workspace_id, scope, name, description, config, is_default, created_by, created_at, updated_at)
SELECT
    mac.workspace_id,
    'personal',
    '个人Default',
    '从个人配置自动迁移的个人默认配置',
    COALESCE(mac.config, '{}'::jsonb),
    true,
    mac.member_id,
    now(),
    now()
FROM member_agent_config mac;
