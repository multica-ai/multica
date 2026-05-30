-- Per-workspace settings for the cerebro Google-identity integration.
--
-- google_signup_domains: lowercased email domains (e.g. 'firtal.com') that
-- are auto-provisioned into this workspace on first Google login. Empty
-- array disables auto-provisioning for this workspace.
--
-- default_role: the member.role assigned to auto-provisioned users. Mirrors
-- the upstream string enum ('owner' | 'admin' | 'member'); validated at the
-- application layer to keep the column free of an additional CHECK that
-- would require coordinated migrations if upstream adds new roles.

CREATE TABLE IF NOT EXISTS cerebro_workspace_auth_settings (
    workspace_id          UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    google_signup_domains TEXT[] NOT NULL DEFAULT '{}',
    default_role          TEXT NOT NULL DEFAULT 'member',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by_user_id    UUID REFERENCES "user"(id) ON DELETE SET NULL
);

-- Lookup index for the auto-provisioning hook: given a new user's email
-- domain, find every workspace that lists that domain. GIN on the array
-- column lets `domain = ANY(google_signup_domains)` use an index instead
-- of a sequential scan as workspace count grows.
CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_auth_settings_domains
    ON cerebro_workspace_auth_settings USING GIN (google_signup_domains);
