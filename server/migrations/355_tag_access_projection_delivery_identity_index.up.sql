CREATE UNIQUE INDEX CONCURRENTLY tag_access_projection_delivery_identity_idx
    ON tag_access_projection_delivery (vibes_workspace_id, authority_version);
