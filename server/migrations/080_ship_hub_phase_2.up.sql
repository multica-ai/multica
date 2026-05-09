-- Ship Hub Phase 2: GitHub webhook ingestion + secret encryption-at-rest.
--
-- Phase 1 was poll-only (every 5 minutes via the reconciler). Phase 2 adds
-- a public webhook endpoint and the supporting tables that make event-
-- driven updates safe at scale:
--
-- - github_webhook_delivery     — at-most-once dedup keyed on GitHub's
--                                 X-GitHub-Delivery header. The receiver
--                                 inserts before processing so a retry
--                                 from GitHub is a no-op.
-- - pull_request_review         — one row per PR review event. Stored
--                                 separately so we can compute the latest
--                                 decision per reviewer (GitHub's "review
--                                 decision" derives from the *current*
--                                 distinct-reviewer set, not the raw
--                                 review log).
-- - pull_request_check          — one row per (PR, head_sha, check name).
--                                 Drives the ci_status rollup on
--                                 pull_request: failure dominates, then
--                                 all-success, otherwise pending.
-- - workspace_secret            — encrypted-at-rest replacement for
--                                 storing github_token (and now the new
--                                 webhook secret) in workspace.settings.
--
-- Reverse-order drop in 080_ship_hub_phase_2.down.sql.

-- Per-workspace HMAC secret for verifying inbound webhook signatures.
-- Stored in plaintext as a fallback if the encryption-key env isn't set;
-- the dedicated workspace_secret table is the preferred storage path.
-- Kept as TEXT here so very early Phase 2 deployments can opt into the
-- secret without first running the encryption migration helper.
ALTER TABLE workspace ADD COLUMN ship_hub_webhook_secret TEXT;

-- Idempotency log for incoming webhook deliveries. delivery_id is the
-- raw X-GitHub-Delivery header (a UUID-shaped string but we keep TEXT
-- so a malformed delivery from a misbehaving sender doesn't blow up the
-- INSERT — we simply dedupe on whatever string they send).
--
-- workspace_id is nullable: a delivery whose signature can't be verified
-- against any known workspace still leaves a forensic record without
-- claiming a workspace ownership. ON DELETE SET NULL preserves the audit
-- trail when a workspace is deleted.
CREATE TABLE github_webhook_delivery (
    delivery_id  TEXT PRIMARY KEY,
    event_type   TEXT NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    workspace_id UUID REFERENCES workspace(id) ON DELETE SET NULL,
    repo_url     TEXT,
    processed_at TIMESTAMPTZ,
    error        TEXT
);
CREATE INDEX idx_github_webhook_delivery_received_at
    ON github_webhook_delivery(received_at DESC);

-- Per-review record. UNIQUE on (pull_request_id, reviewer_login,
-- submitted_at) keeps the upsert idempotent even if GitHub re-delivers
-- the same review (it never reuses submitted_at within a review, but the
-- triple is the safest dedup key).
CREATE TABLE pull_request_review (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    pull_request_id     UUID NOT NULL REFERENCES pull_request(id) ON DELETE CASCADE,
    reviewer_login      TEXT NOT NULL,
    reviewer_avatar_url TEXT,
    -- Open string mirrors GitHub's review state enum:
    -- "APPROVED" | "CHANGES_REQUESTED" | "COMMENTED" | "DISMISSED" | "PENDING".
    state               TEXT NOT NULL,
    body                TEXT,
    submitted_at        TIMESTAMPTZ NOT NULL,
    UNIQUE (pull_request_id, reviewer_login, submitted_at)
);
CREATE INDEX idx_pull_request_review_pr
    ON pull_request_review(pull_request_id, submitted_at DESC);

-- Per-check_run snapshot per (PR, head_sha, name). The head_sha changes
-- whenever a new commit is pushed; older shas' checks linger here only
-- until the next derivation run that ignores them — a periodic prune
-- isn't worth the complexity at this point.
CREATE TABLE pull_request_check (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    pull_request_id UUID NOT NULL REFERENCES pull_request(id) ON DELETE CASCADE,
    head_sha        TEXT NOT NULL,
    name            TEXT NOT NULL,
    -- "success" | "failure" | "neutral" | "cancelled" | "skipped" |
    -- "timed_out" | "" (in_progress / queued, no conclusion yet).
    conclusion      TEXT,
    -- "queued" | "in_progress" | "completed".
    status          TEXT NOT NULL,
    details_url     TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pull_request_id, head_sha, name)
);
CREATE INDEX idx_pull_request_check_pr_head
    ON pull_request_check(pull_request_id, head_sha);

-- Encrypted secret store. Replaces stashing tokens in
-- workspace.settings JSON. value_encrypted is AES-256-GCM with the
-- 12-byte nonce prepended, encoded by the server-side secrets package.
-- Migration logic in cmd/server moves any pre-Phase-2
-- ship_hub.github_token entries here on startup.
CREATE TABLE workspace_secret (
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- Logical name: 'github_token' | 'github_webhook_secret'. Open string
    -- so future Ship Hub-adjacent secrets (Vercel deploy keys etc.) don't
    -- need a schema change.
    name            TEXT NOT NULL,
    value_encrypted BYTEA NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, name)
);
