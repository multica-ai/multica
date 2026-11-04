-- The governance log (spec 4.5): one row per action that changes how the company
-- is governed. Append-only: no query updates a row or deletes one, except the
-- workspace teardown, which removes a workspace's log with the workspace.
--
-- No primary key, as spec 4.5 has none: a primary key would build its index
-- without CONCURRENTLY. id is a random UUID and the cursor orders by
-- (created_at, id). No foreign keys: the writer checks actor and subject.
CREATE TABLE IF NOT EXISTS governance_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id UUID,
    subject_type TEXT NOT NULL,
    subject_id UUID,
    summary TEXT NOT NULL CHECK (char_length(summary) <= 500),
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
