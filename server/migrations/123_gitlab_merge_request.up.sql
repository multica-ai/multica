-- GitLab Merge Request integration: mirrored merge request state and
-- the link table joining issues <-> merge requests.

CREATE TABLE multica_gitlab_merge_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES multica_workspace(id) ON DELETE CASCADE,
    repo_owner        TEXT NOT NULL,
    repo_name         TEXT NOT NULL,
    mr_number         INT NOT NULL,
    mr_id             BIGINT NOT NULL,
    project_id        BIGINT NOT NULL,
    title             TEXT NOT NULL,
    description       TEXT,
    state             TEXT NOT NULL CHECK (state IN ('opened', 'merged', 'closed')),
    html_url          TEXT NOT NULL,
    source_branch     TEXT,
    target_branch     TEXT,
    author_login      TEXT,
    author_avatar_url TEXT,
    merged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,
    mr_created_at     TIMESTAMPTZ NOT NULL,
    mr_updated_at     TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, repo_owner, repo_name, mr_number)
);

CREATE INDEX idx_gitlab_mr_workspace ON multica_gitlab_merge_request(workspace_id);

CREATE TABLE multica_issue_merge_request (
    issue_id         UUID NOT NULL REFERENCES multica_issue(id) ON DELETE CASCADE,
    merge_request_id UUID NOT NULL REFERENCES multica_gitlab_merge_request(id) ON DELETE CASCADE,
    linked_by_type   TEXT,
    linked_by_id     UUID,
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, merge_request_id)
);

CREATE INDEX idx_issue_mr_mr ON multica_issue_merge_request(merge_request_id);
