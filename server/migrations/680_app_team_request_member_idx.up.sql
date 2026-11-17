-- One ask per member and app; the upsert key of a team account request.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_app_team_request_member
    ON app_team_request (workspace_id, toolkit_slug, requested_by);
