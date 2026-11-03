CREATE INDEX CONCURRENTLY IF NOT EXISTS initiative_stage_event_timeline_idx ON initiative_stage_event (workspace_id, initiative_id, created_at);
