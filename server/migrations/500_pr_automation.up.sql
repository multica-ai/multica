-- Explicit PR policy is isolated from the workspace settings blob so older
-- clients cannot overwrite it when saving unrelated settings.
CREATE TABLE pr_automation_policy (
    workspace_id UUID NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('manual', 'title_branch', 'all')),
    auto_complete BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    checked_at TIMESTAMPTZ,
    last_error TEXT
);
CREATE TABLE pr_automation_evidence (
    pr_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    body TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    sync_attempted_at TIMESTAMPTZ,
    sync_error TEXT
);
CREATE TABLE pr_automation_override (
    issue_id UUID NOT NULL,
    pr_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('manual', 'excluded'))
);
CREATE TABLE pr_automation_issue (
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    disabled BOOLEAN NOT NULL DEFAULT FALSE
);

-- Retain identity across disconnect/reconnect without retaining credentials.
CREATE TABLE pr_automation_connection (
    workspace_id UUID NOT NULL,
    instance_url TEXT NOT NULL,
    connection_id UUID NOT NULL
);
