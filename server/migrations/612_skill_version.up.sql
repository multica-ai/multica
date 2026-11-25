-- Append-only content history for workspace skills (MUL-4066).
--
-- Every write that changes a skill's name, description, SKILL.md content, or
-- supporting files appends one immutable row holding a full snapshot of those
-- fields. The live skill / skill_file rows stay the source of truth for task
-- execution; this table only powers the version list and restore. Restoring
-- an old version writes its snapshot back to skill / skill_file and appends a
-- new row, so history only moves forward.
--
-- Skills that predate this table get their first row lazily: the first write
-- after deploy snapshots the pre-write state as a 'baseline' version before
-- applying the change. No backfill runs at migration time.
--
-- No FK, no cascade (workspace DB rules): DeleteSkill and workspace teardown
-- remove a skill's versions explicitly.
CREATE TABLE IF NOT EXISTS skill_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    skill_id UUID NOT NULL,
    version INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    files JSONB NOT NULL DEFAULT '[]'::jsonb,
    content_hash TEXT NOT NULL,
    source TEXT NOT NULL,
    restored_from_version INTEGER NULL,
    created_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
