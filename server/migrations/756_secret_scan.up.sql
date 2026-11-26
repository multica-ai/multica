-- Secret scanning (MUL-10283, server/ee/secretscan): API keys, tokens and
-- private keys found in comments.
--
-- A finding never holds the secret: only its rule, a masked preview, and an
-- HMAC fingerprint keyed by the workspace's salt, which tells two comments
-- holding the same key apart from two holding different ones.
--
-- No foreign keys (repo rule): the scanner deletes a comment's findings when
-- the comment or its issue is deleted, reads join live comments, and workspace
-- teardown removes both tables. Indexes are built concurrently in 757-759.
CREATE TABLE IF NOT EXISTS secret_finding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    comment_id UUID NOT NULL,
    author_type TEXT NOT NULL,
    author_id UUID NOT NULL,
    rule TEXT NOT NULL,
    masked TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    -- resolved: the secret left the comment through an edit.
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'redacted', 'dismissed', 'resolved')),
    resolved_by_type TEXT,
    resolved_by_id UUID,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS secret_scan_setting (
    workspace_id UUID PRIMARY KEY,
    -- notify records findings; redact also replaces them before the comment is stored.
    mode TEXT NOT NULL DEFAULT 'notify' CHECK (mode IN ('notify', 'redact')),
    salt BYTEA NOT NULL DEFAULT gen_random_bytes(32),
    backfill_status TEXT NOT NULL DEFAULT 'idle' CHECK (backfill_status IN ('idle', 'running', 'done', 'failed')),
    backfill_cursor UUID,
    backfill_scanned BIGINT NOT NULL DEFAULT 0,
    backfill_started_at TIMESTAMPTZ,
    backfill_finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
