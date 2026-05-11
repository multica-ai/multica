-- Fork-specific: nested projects companion table.
-- Kept separate from project.down.sql so upstream-merges don't conflict.

DROP INDEX IF EXISTS idx_project_nesting_parent;
DROP TABLE IF EXISTS project_nesting;
