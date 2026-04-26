-- 1001_coderabbit_integration.up.sql
--
-- CodeRabbit / GitHub PR-driven status automation.
--
-- Architecture: ONE GitHub App ("bmad-multica-coderabbit-bridge") installed
-- on the user's account/org. Each workspace_repo_binding row pins a workspace
-- to a specific (installation_id, repo_full_name) pair. The App's private
-- key + webhook secret live in env vars (GITHUB_APP_ID, GITHUB_APP_PRIVATE_KEY_PATH,
-- GITHUB_APP_WEBHOOK_SECRET) — NOT in the database.
--
-- - workspace_repo_binding: one row per (workspace, repo) pair. Stores the
--   GitHub App installation_id used to mint installation tokens for API
--   calls (resolving review threads, etc.) and the CR bot's GitHub username
--   (defaults to coderabbitai[bot]).
-- - issue.pr_* columns: reverse-lookup metadata so a webhook can resolve a
--   PR back to its owning issue without a join.
-- - github_webhook_delivery: dedup table keyed on the X-GitHub-Delivery UUID
--   so we can safely accept GitHub's at-least-once redelivery semantics.
--
-- The issue.status CHECK constraint already permits in_review / staged /
-- blocked (see migration 1000), so no enum change is required here.

CREATE TABLE workspace_repo_binding (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    repo_full_name  TEXT NOT NULL,
    installation_id BIGINT NOT NULL,
    cr_bot_username TEXT NOT NULL DEFAULT 'coderabbitai[bot]',
    active          BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workspace_repo_binding_repo_unique UNIQUE (repo_full_name),
    CONSTRAINT workspace_repo_binding_repo_format CHECK (repo_full_name ~ '^[^/]+/[^/]+$'),
    CONSTRAINT workspace_repo_binding_installation_positive CHECK (installation_id > 0)
);

CREATE INDEX idx_workspace_repo_binding_workspace
    ON workspace_repo_binding (workspace_id)
    WHERE active = true;

ALTER TABLE issue
    ADD COLUMN pr_url    TEXT,
    ADD COLUMN pr_number INTEGER,
    ADD COLUMN pr_repo   TEXT;

-- Composite reverse-lookup index. Partial: most issues never have a PR, so
-- we don't pay the index cost for unrelated rows.
CREATE INDEX idx_issue_pr_lookup
    ON issue (pr_repo, pr_number)
    WHERE pr_repo IS NOT NULL;

-- Webhook delivery dedup. The delivery_id is the X-GitHub-Delivery header
-- value (a UUID). Rows older than 7 days are pruned by an out-of-band job
-- (TBD) — for now we accept unbounded growth, which is fine at our volume.
CREATE TABLE github_webhook_delivery (
    delivery_id TEXT PRIMARY KEY,
    repo        TEXT NOT NULL,
    event       TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_webhook_delivery_received
    ON github_webhook_delivery (received_at);
