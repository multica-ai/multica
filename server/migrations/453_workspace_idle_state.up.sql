-- Persist the workspace busy -> idle notification edge across server replicas.
--
-- There is deliberately no foreign key: workspace teardown deletes this row
-- explicitly, in keeping with the repository's application-owned relationship
-- policy. The primary key is attached in migration 455 after migration 454
-- builds its backing unique index concurrently.
CREATE TABLE workspace_idle_state (
    workspace_id UUID NOT NULL,
    busy_generation BIGINT NOT NULL DEFAULT 1 CHECK (busy_generation > 0),
    idle_notified_generation BIGINT NOT NULL DEFAULT 0 CHECK (idle_notified_generation >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A deployment can land while work is already running. Seed those workspaces
-- as busy so the first terminal event can still produce the idle edge.
INSERT INTO workspace_idle_state (workspace_id, busy_generation, idle_notified_generation)
SELECT DISTINCT a.workspace_id, 1, 0
FROM agent_task_queue task
JOIN agent a ON a.id = task.agent_id
WHERE task.issue_id IS NOT NULL
  AND task.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');
