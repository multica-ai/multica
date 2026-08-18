-- Uniqueness: a member hides a given project file at most once. The leading
-- (project_id, attachment_id, user_id) order also serves ListProjectFiles'
-- LEFT JOIN (project_id + attachment_id + user_id equality). Single statement
-- + CONCURRENTLY: CREATE INDEX CONCURRENTLY cannot run in a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_file_hidden_project_attachment_user_uidx
    ON project_file_hidden (project_id, attachment_id, user_id);
