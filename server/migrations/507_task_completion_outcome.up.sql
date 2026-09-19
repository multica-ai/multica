-- A completed issue task and the final answer visible to the user are one
-- database outcome. Historical tasks intentionally have no row here and are
-- interpreted as legacy_unknown rather than guessed from arbitrary comments.
CREATE TABLE agent_task_completion_outcome (
    task_id UUID PRIMARY KEY REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    outcome_kind TEXT NOT NULL CHECK (outcome_kind IN ('final', 'no_response', 'suppressed')),
    content TEXT NOT NULL DEFAULT '',
    result_sha256 TEXT NOT NULL CHECK (result_sha256 ~ '^[0-9a-f]{64}$'),
    final_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    answered_comment_ids UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (outcome_kind = 'suppressed' AND final_comment_id IS NULL)
        OR (outcome_kind <> 'suppressed' AND final_comment_id IS NOT NULL)
    )
);

-- Realtime publication is at-least-once and independently retryable. The
-- durable comment is already readable after commit; this row repairs a crash
-- between the commit and the websocket publication without re-running work.
CREATE TABLE task_completion_outbox (
    task_id UUID PRIMARY KEY REFERENCES agent_task_completion_outcome(task_id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    issue_revision BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'publishing', 'published', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (comment_id)
);

CREATE INDEX idx_task_completion_outbox_pending
ON task_completion_outbox (COALESCE(next_attempt_at, created_at), created_at, task_id)
WHERE state IN ('pending', 'publishing');
