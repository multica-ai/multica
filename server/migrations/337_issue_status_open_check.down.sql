-- Restoring the enum constraint FAILS if any issue sits on a custom status.
-- That is deliberate: silently rewriting those rows to a built-in status would
-- destroy user data. Migrate custom-status issues back to built-ins first.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_format_check;

ALTER TABLE issue
    ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));
