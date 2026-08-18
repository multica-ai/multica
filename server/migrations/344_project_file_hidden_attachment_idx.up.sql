-- Cleanup index: DeleteAttachment reaps a deleted attachment's hidden rows by
-- attachment_id, so the reap stays cheap as the table grows. Single statement
-- + CONCURRENTLY (see 138_issue_title_trgm_index).
CREATE INDEX CONCURRENTLY IF NOT EXISTS project_file_hidden_attachment_idx
    ON project_file_hidden (attachment_id);
