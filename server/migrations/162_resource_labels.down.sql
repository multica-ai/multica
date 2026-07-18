-- Revert issue_label to its pre-162 issue-only shape and drop the resource
-- label junction tables. The two rollback steps that each need their own
-- transaction live in earlier down migrations, because a single migration
-- file cannot mix DML/DDL with CREATE INDEX CONCURRENTLY:
--   * 174.down removes the agent/skill rows, and
--   * 171.down rebuilds the legacy workspace-wide unique name index
--     CONCURRENTLY.
-- Both run before this file (down migrations apply high->low), so the
-- non-issue rows are already gone by the time we drop resource_type here.
DROP TABLE IF EXISTS skill_to_label;
DROP TABLE IF EXISTS agent_to_label;

-- Defensive: a short-lived pre-release deploy may have applied the original
-- 162 that created these indexes inline. On the normal down chain 167/168
-- already dropped them CONCURRENTLY, so these are no-ops.
DROP INDEX IF EXISTS issue_label_workspace_type_idx;
DROP INDEX IF EXISTS issue_label_workspace_type_name_lower_idx;

ALTER TABLE issue_label
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS resource_type;
