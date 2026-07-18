-- Per-connection webhook secret for GitLab X-Gitlab-Token verification.
-- Replaces the shared GITLAB_WEBHOOK_SECRET env var for inbound webhooks so each
-- workspace connection has an isolated credential that can be rotated from the UI.
-- Empty string means "not yet issued" — the server generates one on connect/list
-- or falls back to GITLAB_WEBHOOK_SECRET for legacy rows until rotated.
ALTER TABLE gitlab_connection
    ADD COLUMN IF NOT EXISTS webhook_secret TEXT NOT NULL DEFAULT '';
