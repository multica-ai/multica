-- Lookup index for joins/previews by invite code.
CREATE UNIQUE INDEX CONCURRENTLY workspace_share_link_code_uidx
    ON workspace_share_link (code);
