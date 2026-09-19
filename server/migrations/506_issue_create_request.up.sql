-- Durable request identity for API clients that must create an issue exactly
-- once across retries, process crashes, and concurrent submissions.
--
-- The reservation row and the issue are written in one transaction. A caller
-- that repeats the same key and payload receives the original issue; the same
-- key with different content is rejected instead of silently returning the
-- wrong object.
CREATE TABLE issue_create_request (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    request_key TEXT NOT NULL CHECK (length(request_key) BETWEEN 1 AND 200),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    issue_id UUID REFERENCES issue(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, actor_type, actor_id, request_key)
);

