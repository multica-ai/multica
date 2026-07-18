-- Inverse of 171.up, which drops the pre-162 workspace-wide unique name
-- index. Recreated as a single CREATE INDEX CONCURRENTLY statement so the
-- rollback stays online-safe (it cannot be combined with other statements
-- or run inside a transaction block).
--
-- The agent/skill rows that would violate this workspace-wide uniqueness are
-- removed earlier in the down chain by 174.down (down migrations apply
-- high->low), so only issue rows remain when this index is rebuilt.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_label_workspace_name_lower_idx
    ON issue_label (workspace_id, LOWER(name));
