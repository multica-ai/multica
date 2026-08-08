CREATE TABLE external_pull_request (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    issue_id        UUID NOT NULL,
    provider        TEXT NOT NULL CHECK (provider IN ('code')),
    repository_path TEXT NOT NULL,
    review_number   INTEGER NOT NULL CHECK (review_number > 0),
    title           TEXT NOT NULL,
    html_url        TEXT NOT NULL,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id   UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
