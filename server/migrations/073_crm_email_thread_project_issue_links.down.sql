DROP INDEX IF EXISTS idx_crm_email_thread_issue_time;
DROP INDEX IF EXISTS idx_crm_email_thread_project_time;

ALTER TABLE crm_email_thread
    DROP COLUMN IF EXISTS issue_id,
    DROP COLUMN IF EXISTS project_id;
