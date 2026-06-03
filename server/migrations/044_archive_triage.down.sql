DROP INDEX IF EXISTS idx_inbox_triage_visible;

ALTER TABLE inbox_item
    DROP CONSTRAINT IF EXISTS inbox_item_triage_status_check,
    DROP COLUMN IF EXISTS triaged_by,
    DROP COLUMN IF EXISTS dismissed_at,
    DROP COLUMN IF EXISTS handled_at,
    DROP COLUMN IF EXISTS snoozed_until,
    DROP COLUMN IF EXISTS triage_status;

DROP INDEX IF EXISTS idx_issue_workspace_archived;

ALTER TABLE issue
    DROP COLUMN IF EXISTS archived_by,
    DROP COLUMN IF EXISTS archived_at;
