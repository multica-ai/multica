-- Unify global skills into the skill table.
-- Global skills (reported by daemon runtimes) are now rows in skill with
-- is_global = true and runtime_id set, using skill_file for their files.

ALTER TABLE skill
    ADD COLUMN is_global   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN runtime_id  UUID    REFERENCES agent_runtime(id) ON DELETE CASCADE;

-- Replace the workspace-wide unique constraint with two partial indexes:
-- one for workspace-managed skills and one for global skills.
ALTER TABLE skill DROP CONSTRAINT skill_workspace_id_name_key;

CREATE UNIQUE INDEX skill_workspace_name_unique
    ON skill(workspace_id, name) WHERE NOT is_global;

CREATE UNIQUE INDEX skill_global_runtime_name_unique
    ON skill(runtime_id, name) WHERE is_global;

CREATE INDEX idx_skill_runtime ON skill(runtime_id) WHERE is_global;

-- Migrate existing global skill rows into the skill table.
INSERT INTO skill (workspace_id, runtime_id, name, description, content, config, is_global, created_at, updated_at)
SELECT ar.workspace_id, rgs.runtime_id, rgs.name, rgs.description,
       COALESCE(rgs.content, ''), '{}', true, rgs.created_at, rgs.updated_at
FROM runtime_global_skill rgs
JOIN agent_runtime ar ON ar.id = rgs.runtime_id
ON CONFLICT DO NOTHING;

DROP TABLE IF EXISTS runtime_global_skill;
