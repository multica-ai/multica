-- PUL-94 (per-task git worktree): explicit target_project_resource_id on
-- agent_task_queue. When set, the daemon uses this row (expected
-- resource_type='github_repo') to resolve which bare repo to base the
-- per-task worktree on. When NULL, the daemon falls back to legacy
-- behavior (shared per-agent worktree on /srv/pulse-bare.git).
--
-- ON DELETE SET NULL — removing a project_resource must not cascade-delete
-- queued tasks; the task just loses its explicit target and falls back.
--
-- App-layer enforcement (handler.ClaimTaskByRuntime + enqueue paths):
-- the referenced row must have resource_type='github_repo'. A schema-level
-- CHECK on the polymorphic project_resource table would require a function
-- or trigger; not worth the cost for a single invariant.
--
-- See: plans://Multica/2026-05-12-pul-94-agent-worktree-per-task.md (A1).

ALTER TABLE agent_task_queue
    ADD COLUMN target_project_resource_id UUID NULL
        REFERENCES project_resource(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_task_queue_target_project_resource
    ON agent_task_queue(target_project_resource_id)
    WHERE target_project_resource_id IS NOT NULL;
