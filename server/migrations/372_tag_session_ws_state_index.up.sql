CREATE UNIQUE INDEX CONCURRENTLY tag_access_session_workspace_state_uidx
    ON tag_access_session_workspace_state (vibes_user_id, vibes_session_id);
