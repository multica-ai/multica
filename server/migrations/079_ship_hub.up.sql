-- Ship Hub (Phase 1, read-only): a Kanban-style surface that mirrors GitHub
-- pull-request and deployment state for projects with attached github_repo
-- resources. PRs are pulled from the GitHub REST API on demand and via a
-- periodic reconciler; deploy state is populated manually in v1 and will
-- accept webhook ingestion later.
--
-- Naming + indexing decisions:
--
-- - `pull_request` is identified by (workspace_id, repo_url, pr_number). The
--   repo_url is what a user actually pasted into the project_resource, so it
--   stays the canonical key — switching to (owner, repo) would force every
--   ingest path to re-derive the URL form for joins.
--
-- - `deploy_environment` is one row per (project_id, kind). UNIQUE constraint
--   makes the manual sync endpoint idempotent — the same project can't have
--   two "staging" rows, and an upsert in the service layer is a single query.
--
-- - `deploy` is append-only (mostly). Each attempt is its own row so the UI
--   can render history without losing prior state when a deploy fails and is
--   retried.

CREATE TYPE pull_request_state AS ENUM ('open', 'closed', 'merged');
CREATE TYPE deploy_environment_kind AS ENUM ('staging', 'production');
CREATE TYPE deploy_status AS ENUM ('pending', 'in_progress', 'succeeded', 'failed', 'rolled_back');

-- One row per PR, identified by (repo_url, pr_number) within a workspace.
-- project_id is nullable so a PR row survives the project being deleted —
-- we still want the row for history / audit; the upsert path will re-link
-- when the project is recreated and synced again.
CREATE TABLE pull_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id        UUID REFERENCES project(id) ON DELETE SET NULL,
    repo_url          TEXT NOT NULL,
    pr_number         INTEGER NOT NULL,
    title             TEXT NOT NULL,
    state             pull_request_state NOT NULL,
    is_draft          BOOLEAN NOT NULL DEFAULT FALSE,
    author_login      TEXT NOT NULL,
    author_avatar_url TEXT,
    base_ref          TEXT NOT NULL,
    head_ref          TEXT NOT NULL,
    head_sha          TEXT NOT NULL,
    html_url          TEXT NOT NULL,
    body              TEXT,
    -- GitHub combined commit-status string. Open string (not enum) because
    -- GitHub adds new statuses occasionally and forcing an enum migration
    -- on every new value would be painful.
    ci_status         TEXT,
    -- "APPROVED" | "CHANGES_REQUESTED" | "REVIEW_REQUIRED" | "" (empty when unknown).
    review_decision   TEXT,
    -- "MERGEABLE" | "CONFLICTING" | "UNKNOWN".
    mergeable         TEXT,
    additions         INTEGER NOT NULL DEFAULT 0,
    deletions         INTEGER NOT NULL DEFAULT 0,
    changed_files     INTEGER NOT NULL DEFAULT 0,
    labels            JSONB NOT NULL DEFAULT '[]'::jsonb,
    pr_created_at     TIMESTAMPTZ NOT NULL,
    pr_updated_at     TIMESTAMPTZ NOT NULL,
    pr_merged_at      TIMESTAMPTZ,
    pr_closed_at      TIMESTAMPTZ,
    -- our metadata: when did we last sync this row from GitHub. Used by the
    -- reconciler's "skip if recently synced" check + UI freshness indicator.
    fetched_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, repo_url, pr_number)
);

-- Workspace-scoped open-PR list (the default sidebar/Kanban view).
CREATE INDEX idx_pull_request_workspace_state
    ON pull_request(workspace_id, state, pr_updated_at DESC);

-- Project-scoped lookups (Ship Hub per-project view + open count badge).
-- Partial: NULL project_id rows are orphans from a deleted project; never
-- queried by project page.
CREATE INDEX idx_pull_request_project
    ON pull_request(project_id)
    WHERE project_id IS NOT NULL;

-- One row per (project, environment kind). E.g. a Multica project has one
-- "staging" row and one "production" row.
CREATE TABLE deploy_environment (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    kind                deploy_environment_kind NOT NULL,
    name                TEXT NOT NULL,
    target_branch       TEXT NOT NULL DEFAULT 'main',
    target_url          TEXT,
    -- Last reported successfully-deployed SHA. Updated via the manual sync
    -- endpoint or the reconciler in a later phase.
    current_sha         TEXT,
    current_deployed_at TIMESTAMPTZ,
    auto_promote        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, kind)
);

CREATE INDEX idx_deploy_environment_workspace
    ON deploy_environment(workspace_id);

-- Append-only history of deploy attempts. The service layer transitions
-- status as state evolves (pending → in_progress → succeeded/failed). On
-- success it also bumps the parent deploy_environment.current_sha so the
-- "what's running right now" answer is a single column read.
CREATE TABLE deploy (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    environment_id UUID NOT NULL REFERENCES deploy_environment(id) ON DELETE CASCADE,
    ref            TEXT NOT NULL,
    sha            TEXT NOT NULL,
    status         deploy_status NOT NULL,
    triggered_by   UUID REFERENCES "user"(id),
    triggered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    log_url        TEXT,
    error_message  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_deploy_environment_status
    ON deploy(environment_id, triggered_at DESC);

-- Workspace-level Ship Hub flag. Mirrors channels_enabled (#065): defaults
-- FALSE so the feature surface is invisible until a workspace owner opts in.
ALTER TABLE workspace
    ADD COLUMN ship_hub_enabled BOOLEAN NOT NULL DEFAULT FALSE;
