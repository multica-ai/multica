-- JEH-1076: public-link share tokens. One row per minted link.
-- Mapping is: token -> (resource_kind, resource_id, action) within a workspace.
-- The matching anonymous-grant in firtal-persona is keyed by persona_grant_id
-- so revoking a share-token also revokes the persona allow.
CREATE TABLE IF NOT EXISTS cerebro_share_token (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    token             TEXT NOT NULL UNIQUE,
    resource_kind     TEXT NOT NULL,
    resource_id       UUID NOT NULL,
    action            TEXT NOT NULL DEFAULT 'read',
    persona_grant_id  TEXT NOT NULL DEFAULT '',
    created_by_id     UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_cerebro_share_token_workspace
    ON cerebro_share_token (workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_share_token_resource
    ON cerebro_share_token (workspace_id, resource_kind, resource_id);
