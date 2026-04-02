-- 032: Runtime ownership, visibility, and task approval
--
-- Adds:
--   agent_runtime.owner_id      — who registered this runtime
--   agent_runtime.visibility    — 'workspace' (default) or 'private'
--   agent.approval_required     — whether cross-user tasks need approval
--   agent_task_queue.requested_by — who triggered the task
--   agent_task_queue.status     — adds 'pending_approval' value
--   Updated unique index to include pending_approval

-- 1. agent_runtime: owner_id
ALTER TABLE agent_runtime ADD COLUMN owner_id UUID REFERENCES "user"(id);

-- 2. agent_runtime: visibility with CHECK constraint
ALTER TABLE agent_runtime ADD COLUMN visibility TEXT NOT NULL DEFAULT 'workspace';
ALTER TABLE agent_runtime ADD CONSTRAINT agent_runtime_visibility_check
    CHECK (visibility IN ('workspace', 'private'));

-- 3. agent: approval_required
ALTER TABLE agent ADD COLUMN approval_required BOOLEAN NOT NULL DEFAULT false;

-- 4. agent_task_queue: requested_by
ALTER TABLE agent_task_queue ADD COLUMN requested_by UUID REFERENCES "user"(id);

-- 5. agent_task_queue: expand status CHECK to include 'pending_approval'
ALTER TABLE agent_task_queue DROP CONSTRAINT agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('pending_approval', 'queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled'));

-- 6. Update unique index to also block duplicate pending_approval tasks per issue
DROP INDEX IF EXISTS idx_one_pending_task_per_issue;
CREATE UNIQUE INDEX idx_one_pending_task_per_issue
    ON agent_task_queue (issue_id)
    WHERE status IN ('pending_approval', 'queued', 'dispatched');

-- 7. Update cancel queries coverage: also cancel pending_approval in CancelAgentTasksByIssue/Agent
-- (Handled in SQL query updates, not schema — the CHECK constraint now allows the status)
