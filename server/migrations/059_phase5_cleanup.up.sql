-- Phase 5 cleanup: drop dead legacy schema.
--
-- issue_dependency: pre-GitLab feature that never shipped. Zero runtime
-- references in the codebase.
--
-- issue.origin_type / origin_id: replaced by autopilot_issue mapping in
-- Phase 4. Listener fallback reading these columns was removed in the
-- same commit batch as this migration.

DROP TABLE IF EXISTS issue_dependency;

DROP INDEX IF EXISTS idx_issue_origin;
ALTER TABLE issue DROP COLUMN IF EXISTS origin_type;
ALTER TABLE issue DROP COLUMN IF EXISTS origin_id;
