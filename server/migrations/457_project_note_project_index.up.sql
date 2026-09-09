-- Listing a project's notepads is the hot read (note list UI + `multica project
-- note list`), ordered by position. Built concurrently and in its own migration
-- file because CREATE INDEX CONCURRENTLY cannot run inside a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_note_project
    ON project_note (project_id, position);
