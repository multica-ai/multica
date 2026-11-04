-- One chief per floor. Also the lookup the floor digest and the brief
-- publish check make.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS floor_chief_floor_uidx ON floor_chief (workspace_id, floor_id);
