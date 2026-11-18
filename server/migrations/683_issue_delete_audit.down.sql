-- Roll back migration 683, retaining deletion archives deliberately: audit
-- history must survive the application/schema rollback.
DROP TRIGGER IF EXISTS trg_issue_archive_on_delete ON issue;
DROP TRIGGER IF EXISTS trg_issue_archive_append_only ON issue_archive;
DROP TRIGGER IF EXISTS trg_issue_archive_truncate_only ON issue_archive;
DROP TRIGGER IF EXISTS trg_issue_archive_comment_append_only ON issue_archive_comment;
DROP TRIGGER IF EXISTS trg_issue_archive_comment_truncate_only ON issue_archive_comment;
DROP FUNCTION IF EXISTS archive_issue_on_delete();
DROP FUNCTION IF EXISTS prevent_issue_archive_mutation();

ALTER TABLE activity_log
    DROP CONSTRAINT IF EXISTS activity_log_issue_id_fkey;
ALTER TABLE activity_log
    ADD CONSTRAINT activity_log_issue_id_fkey
    FOREIGN KEY (issue_id) REFERENCES issue(id) ON DELETE CASCADE
    NOT VALID;
ALTER TABLE activity_log
    VALIDATE CONSTRAINT activity_log_issue_id_fkey;

ALTER TABLE activity_log
    DROP COLUMN IF EXISTS issue_number,
    DROP COLUMN IF EXISTS issue_title;
