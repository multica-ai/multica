-- Floors are always read for one workspace in position order.
CREATE INDEX CONCURRENTLY IF NOT EXISTS office_floor_workspace_position_idx ON office_floor (workspace_id, position);
