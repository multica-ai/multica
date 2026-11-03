CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS initiative_artifact_key_uidx ON initiative_artifact (initiative_id, stage_key, kind);
