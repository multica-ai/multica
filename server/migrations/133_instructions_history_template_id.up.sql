-- Instructions history: key by template instead of (scope, member).
--
-- The default config of a scope IS the default template (migration 125), so
-- instructions history — which tracks the default config's instructions over
-- time — belongs to a template, not to a (scope, member) pair. Keying by
-- template lets EVERY template (not just the two defaults) carry its own
-- instructions history.
--
-- Existing rows are backfilled to their scope's default template; rows that
-- cannot be mapped (a member with history but no default personal template —
-- only possible for legacy data predating migration 125) are orphans and
-- dropped. scope/member_id columns are kept for now (legacy write paths in
-- agent_defaults.go / workspace.go still populate them); they are removed
-- when those legacy paths are deleted.
--
-- Idempotent for preview DBs that applied 127_instructions_history_template_id
-- before this file was renumbered to 133.

ALTER TABLE instructions_history ADD COLUMN IF NOT EXISTS template_id UUID;

-- system rows -> the workspace's default system template
UPDATE instructions_history ih
SET template_id = t.id
FROM agent_config_template t
WHERE ih.workspace_id = t.workspace_id
  AND ih.scope = 'system'
  AND t.scope = 'system' AND t.is_default = true
  AND ih.template_id IS NULL;

-- personal rows -> that member's default personal template (member_id == created_by)
UPDATE instructions_history ih
SET template_id = t.id
FROM agent_config_template t
WHERE ih.workspace_id = t.workspace_id
  AND ih.scope = 'personal'
  AND t.scope = 'personal' AND t.is_default = true
  AND ih.member_id = t.created_by
  AND ih.template_id IS NULL;

-- drop unmappable orphans (no default template to attach to)
DELETE FROM instructions_history WHERE template_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'instructions_history'
          AND column_name = 'template_id'
          AND is_nullable = 'YES'
    ) THEN
        ALTER TABLE instructions_history ALTER COLUMN template_id SET NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_instructions_history_template'
    ) THEN
        ALTER TABLE instructions_history
          ADD CONSTRAINT fk_instructions_history_template
          FOREIGN KEY (template_id) REFERENCES agent_config_template(id) ON DELETE CASCADE;
    END IF;
END $$;
