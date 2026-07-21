CREATE TABLE IF NOT EXISTS gitlab_connection (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    namespace        TEXT NOT NULL,
    namespace_type   TEXT NOT NULL CHECK (namespace_type IN ('group', 'user')),
    avatar_url       TEXT,
    access_token     TEXT NOT NULL,
    token_expires_at TIMESTAMPTZ,
    connected_by_id  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, namespace)
);

CREATE INDEX IF NOT EXISTS idx_gitlab_connection_workspace ON gitlab_connection(workspace_id);

CREATE TABLE IF NOT EXISTS gitlab_merge_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id     UUID NOT NULL REFERENCES gitlab_connection(id) ON DELETE CASCADE,
    project_path      TEXT NOT NULL,
    mr_iid            INTEGER NOT NULL,
    title             TEXT NOT NULL,
    state             TEXT NOT NULL CHECK (state IN ('open', 'closed', 'merged', 'locked')),
    html_url          TEXT NOT NULL,
    source_branch     TEXT,
    author_username   TEXT,
    author_avatar_url TEXT,
    merged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,
    mr_created_at     TIMESTAMPTZ NOT NULL,
    mr_updated_at     TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_path, mr_iid)
);

CREATE INDEX IF NOT EXISTS idx_gitlab_merge_request_workspace ON gitlab_merge_request(workspace_id);
CREATE INDEX IF NOT EXISTS idx_gitlab_merge_request_connection ON gitlab_merge_request(connection_id);

CREATE TABLE IF NOT EXISTS issue_merge_request (
    issue_id         UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    merge_request_id UUID NOT NULL REFERENCES gitlab_merge_request(id) ON DELETE CASCADE,
    close_intent     BOOLEAN NOT NULL DEFAULT false,
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, merge_request_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_merge_request_mr ON issue_merge_request(merge_request_id);
