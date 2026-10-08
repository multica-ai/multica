-- PR auto-complete (MUL-7429). Two small per-issue records:
--
--   issue_pr_automation: an explicit "do not auto-complete this issue from PR
--   merges" choice. Absent row = follow the workspace setting.
--
--   issue_pull_request_exclusion: a PR a person removed from an issue. The
--   webhook must not re-link it from the title/branch on the next delivery.
--   pull_request_id points at github_pull_request or vcs_pull_request (both use
--   uuid ids).
--
-- No foreign keys: issue and workspace deletion are application-owned and
-- sweep both tables explicitly (DeleteIssue, DeleteWorkspaceData).
CREATE TABLE IF NOT EXISTS issue_pr_automation (
    issue_id               UUID PRIMARY KEY,
    workspace_id           UUID NOT NULL,
    auto_complete_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by_type        TEXT,
    updated_by_id          UUID,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS issue_pull_request_exclusion (
    issue_id         UUID NOT NULL,
    pull_request_id  UUID NOT NULL,
    workspace_id     UUID NOT NULL,
    excluded_by_type TEXT,
    excluded_by_id   UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, pull_request_id)
);
