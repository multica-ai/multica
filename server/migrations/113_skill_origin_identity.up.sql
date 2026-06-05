-- Migration: 113_skill_origin_identity
-- A skill's identity was its name (UNIQUE(workspace_id, name) from migration
-- 008), so all dedup keyed on name. That is wrong once skills have an external
-- source: importing a skill whose name collides with a DIFFERENT-source skill
-- silently reused the wrong one. This migration introduces an `origin` column
-- as the real identity.
--
--   - Imported / template-imported skills: origin = the raw source URL already
--     stored in config.origin.source_url.
--   - Hand-authored skills: origin = 'local:' || name.
--
-- Same origin = same skill (reuse); different origin = different skill even
-- when names match (coexist).

-- Step 1: add the column nullable so we can backfill before enforcing NOT NULL.
ALTER TABLE skill ADD COLUMN origin TEXT;

-- Step 2: backfill imported skills from their stored provenance. The key we
-- store here MUST be byte-identical to what new ImportSkill / template writes
-- use, so the lookup hits — both read config.origin.source_url.
UPDATE skill
SET origin = config->'origin'->>'source_url'
WHERE config->'origin'->>'source_url' IS NOT NULL
  AND origin IS NULL;

-- Step 3: backfill hand-authored skills (no provenance) as local:<name>.
UPDATE skill
SET origin = 'local:' || name
WHERE origin IS NULL;

-- Step 4: dedupe colliding origins BEFORE adding the unique constraint. The old
-- constraint only blocked same name, so two rows can share a source_url (e.g.
-- same URL imported twice under names that happened to differ). Do NOT delete
-- rows — they may be bound to agents via agent_skill. Suffix later duplicates
-- with '#<n>' so origin stays unique, keeping the earliest row unchanged.
WITH dups AS (
  SELECT id, ROW_NUMBER() OVER (PARTITION BY workspace_id, origin ORDER BY created_at, id) AS rn FROM skill
)
UPDATE skill s SET origin = s.origin || '#' || d.rn FROM dups d WHERE s.id = d.id AND d.rn > 1;

-- Step 5: now that every row has a unique origin, enforce NOT NULL.
ALTER TABLE skill ALTER COLUMN origin SET NOT NULL;

-- Step 6: swap the identity constraint from name to origin. The old constraint
-- is the auto-generated name from the inline UNIQUE(workspace_id, name) in
-- migration 008. Drop it with a guard for robustness across environments.
ALTER TABLE skill DROP CONSTRAINT IF EXISTS skill_workspace_id_name_key;
ALTER TABLE skill ADD CONSTRAINT skill_workspace_origin_unique UNIQUE (workspace_id, origin);
