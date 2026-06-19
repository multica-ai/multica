-- Fork-specific: FIR-1614 — widen project nesting to 4 levels and add a
-- per-sibling ordering column so the sidebar tree can be reordered by drag.

-- 4 levels = absolute depth 0..3 (was capped at 0..2 / 3 levels).
ALTER TABLE project_nesting DROP CONSTRAINT IF EXISTS project_nesting_depth_check;
ALTER TABLE project_nesting ADD CONSTRAINT project_nesting_depth_check CHECK (depth BETWEEN 0 AND 3);

-- Per-sibling display order (lower = first). Projects without a nesting row
-- fall back to position 0 and are then ordered by title.
ALTER TABLE project_nesting ADD COLUMN IF NOT EXISTS position INT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_project_nesting_parent_position
    ON project_nesting(parent_project_id, position);
