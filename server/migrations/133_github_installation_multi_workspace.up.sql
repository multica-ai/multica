-- Allow one GitHub App installation to bind to multiple workspaces.
--
-- Previously UNIQUE(installation_id) forced a single workspace per installation.
-- Connecting the same GitHub account/org in a second workspace ran the
-- CreateGitHubInstallation upsert, whose ON CONFLICT (installation_id) silently
-- overwrote the first workspace's binding row (#4823). Widening the uniqueness
-- key to (workspace_id, installation_id) lets each workspace keep its own row,
-- and webhook delivery is routed per-repo via the workspace.repos registry.
ALTER TABLE github_installation
    DROP CONSTRAINT github_installation_installation_id_key;

ALTER TABLE github_installation
    ADD CONSTRAINT github_installation_workspace_id_installation_id_key
    UNIQUE (workspace_id, installation_id);

-- The dropped UNIQUE(installation_id) also provided the index behind webhook
-- lookups by installation_id. The new composite constraint is keyed on
-- workspace_id first, so it cannot serve a bare installation_id lookup — add a
-- standalone index to keep that path fast.
CREATE INDEX IF NOT EXISTS idx_github_installation_installation_id
    ON github_installation(installation_id);
