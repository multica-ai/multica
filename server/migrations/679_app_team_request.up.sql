-- A member asking the workspace's admins to connect a team account for an
-- app. One row per member and app; asking again refreshes it. Connecting the
-- team account or an admin dismissing the ask deletes the app's rows.
--
-- No foreign keys (repo rule): workspace teardown and member removal delete
-- these rows explicitly. The unique index is built concurrently in 680.
CREATE TABLE IF NOT EXISTS app_team_request (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    toolkit_slug TEXT NOT NULL,
    requested_by UUID NOT NULL,
    -- The agent the member wants the app for, if they named one.
    agent_id     UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
