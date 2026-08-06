-- Jira integration (PR 1: foundation): per-workspace API-token connections to
-- a Jira Cloud (or Data Center) site plus the link table that maps a Jira
-- issue to the Multica issue mirrored from it.
--
-- Follows the VCS integration model (migration 216): each workspace stores
-- the site base URL, the account email + API token used for REST enrichment,
-- and a webhook secret used to authenticate inbound webhook deliveries (Jira
-- webhooks carry no native signature, so Multica mints a secret the operator
-- embeds in the webhook URL / header).
--
-- Both secrets are stored as base64-encoded secretbox ciphertext (never
-- plaintext). Decryption uses the MULTICA_JIRA_SECRET_KEY box wired in
-- cmd/server/router.go.
--
-- Per the project migration rules these tables carry NO foreign keys or
-- cascades: relationships and dependent cleanup are resolved in application
-- code (DeleteJiraConnection sweeps jira_issue_link in a single atomic
-- statement). The inline UNIQUE / PRIMARY KEY constraints stay — they back
-- the ON CONFLICT upsert targets in jira.sql. Secondary indexes live in
-- follow-up single-statement CREATE INDEX CONCURRENTLY migrations (225-227),
-- which cannot share a file with these CREATE TABLEs.

CREATE TABLE IF NOT EXISTS jira_connection (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL,
    base_url                 TEXT NOT NULL,
    account_email            TEXT NOT NULL,
    api_token_encrypted      TEXT NOT NULL,
    webhook_secret_encrypted TEXT NOT NULL,
    connected_by_id          UUID,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, base_url)
);

CREATE TABLE IF NOT EXISTS jira_issue_link (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    connection_id    UUID NOT NULL,
    jira_issue_key   TEXT NOT NULL,
    jira_issue_id    TEXT NOT NULL,
    multica_issue_id UUID NOT NULL,
    sync_status      TEXT NOT NULL DEFAULT 'synced'
        CHECK (sync_status IN ('synced', 'pending', 'error')),
    last_inbound_at  TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connection_id, jira_issue_key)
);
