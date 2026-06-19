-- Revert FIR-1614 project nesting order + 4-level changes.
DROP INDEX IF EXISTS idx_project_nesting_parent_position;
ALTER TABLE project_nesting DROP COLUMN IF EXISTS position;

ALTER TABLE project_nesting DROP CONSTRAINT IF EXISTS project_nesting_depth_check;
ALTER TABLE project_nesting ADD CONSTRAINT project_nesting_depth_check CHECK (depth BETWEEN 0 AND 2);
