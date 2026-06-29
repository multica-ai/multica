-- Azure DevOps integration: workspace-level PAT installations, mirrored PR
-- state (including policy evaluation results), and CI build check records.
-- Mirrors the github_installation / github_pull_request / check_suite
-- structure so the frontend can reuse the same PR-panel component with a
-- provider discriminator.

-- ── workspace ↔ ADO org connection ─────────────────────────────────────────
-- One row per (workspace, ADO org URL). A workspace can connect multiple ADO
-- organisations (multi-org parity with GitHub's multi-installation model).
-- v1: PAT-only (pat_encrypted). v2 will add oauth_token_encrypted.
-- webhook_secret is auto-generated at insert time (stored as hex-encoded
-- random bytes); it is used as the Basic-Auth password on the ADO service-
-- hook URL so Multica can verify incoming events are genuine.

CREATE TABLE ado_installation (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- Canonical org URL: always https://dev.azure.com/{org}
    org_url         TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    -- PAT stored AES-256-GCM encrypted; nonce prepended (12-byte || ciphertext).
    -- NULL when this row was created via OAuth (v2).
    pat_encrypted   BYTEA,
    -- Webhook secret sent as Basic Auth password on the ADO service-hook URL.
    -- Auto-generated (random 32 bytes, hex-encoded) so the operator never has
    -- to configure it manually.
    webhook_secret  TEXT NOT NULL DEFAULT encode(gen_random_bytes(32), 'hex'),
    connected_by_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, org_url)
);

CREATE INDEX idx_ado_installation_workspace ON ado_installation(workspace_id);

-- ── ADO pull request mirror ─────────────────────────────────────────────────
-- Mirrors the github_pull_request table structure so the same issue-PR-panel
-- query (ListPullRequestsByIssue) and broadcast events work for both providers.
-- Key ADO additions:
--   policy_status  – aggregated gate result ("approved"/"blocked"/"pending"/NULL)
--   pr_id_ado      – ADO's integer PR id (unique within a repo)
--   repo_id_ado    – ADO's GUID for the repository (stable across renames)

CREATE TABLE ado_pull_request (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id     UUID NOT NULL REFERENCES ado_installation(id) ON DELETE CASCADE,
    org_url             TEXT NOT NULL,
    project             TEXT NOT NULL,
    repo_name           TEXT NOT NULL,
    repo_id_ado         TEXT NOT NULL,  -- ADO repository GUID
    pr_id_ado           INTEGER NOT NULL,
    title               TEXT NOT NULL,
    state               TEXT NOT NULL
        CHECK (state IN ('open', 'closed', 'merged', 'draft', 'abandoned')),
    html_url            TEXT NOT NULL,
    branch              TEXT,
    author_login        TEXT,
    author_avatar_url   TEXT,
    merged_at           TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    pr_created_at       TIMESTAMPTZ NOT NULL,
    pr_updated_at       TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- ADO-specific fields
    -- policy_status: aggregated result of all required PR policy evaluations.
    -- "approved"  – all required policies passed, PR can merge.
    -- "blocked"   – at least one required policy is rejected/failed.
    -- "pending"   – policies are still running (queued/running).
    -- NULL        – no policy data received yet.
    policy_status       TEXT
        CHECK (policy_status IS NULL OR policy_status IN ('approved', 'blocked', 'pending')),
    merge_status        TEXT,  -- ADO mergeStatus: conflicts/blocked/rejectedByPolicy/succeeded/queued
    UNIQUE (workspace_id, org_url, project, repo_name, pr_id_ado)
);

CREATE INDEX idx_ado_pull_request_workspace ON ado_pull_request(workspace_id);
CREATE INDEX idx_ado_pull_request_installation ON ado_pull_request(installation_id);

-- ── issue ↔ ADO PR link (reuses existing issue_pull_request convention) ──────
-- Separate table so GitHub and ADO links are independently queryable but the
-- same close-intent / auto-advance logic applies.

CREATE TABLE issue_ado_pull_request (
    issue_id            UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    pull_request_id     UUID NOT NULL REFERENCES ado_pull_request(id) ON DELETE CASCADE,
    close_intent        BOOLEAN NOT NULL DEFAULT false,
    linked_by_type      TEXT,
    linked_by_id        UUID,
    linked_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, pull_request_id)
);

CREATE INDEX idx_issue_ado_pr ON issue_ado_pull_request(pull_request_id);

-- ── ADO build check record ──────────────────────────────────────────────────
-- Stores the last known result per (pr_id, build_definition_id) tuple so the
-- UI can show a segmented check-bar identical to the GitHub check-suite bar.

CREATE TABLE ado_pull_request_build_check (
    pr_id               UUID NOT NULL REFERENCES ado_pull_request(id) ON DELETE CASCADE,
    build_id            BIGINT NOT NULL,           -- ADO build.id
    definition_id       INTEGER NOT NULL,          -- ADO buildDefinition.id
    definition_name     TEXT NOT NULL DEFAULT '',
    conclusion          TEXT,                      -- succeeded/failed/canceled/partiallySucceeded
    status              TEXT NOT NULL DEFAULT '',  -- completed/inProgress/notStarted
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (pr_id, build_id)
);
