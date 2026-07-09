-- Adds task-level retry/lease bookkeeping so the runtime sweeper and the
-- daemon startup recovery path can distinguish "fresh attempt" from
-- "auto-rerun after orphan", and so resume context survives a daemon
-- restart mid-execution.
--
-- Columns:
--   attempt           -- 1 for the first run, incremented per auto-retry/manual rerun
--   max_attempts      -- ceiling honored by the auto-retry path; 1 disables retry
--   parent_task_id    -- back-pointer to the task that this one re-attempts
--   failure_reason    -- coarse classifier set when status flips to failed:
--                        'agent_error', 'timeout', 'runtime_offline',
--                        'runtime_recovery', 'manual'. The auto-retry path
--                        uses this to decide whether to spawn a child task.
--   last_heartbeat_at -- mid-task heartbeat timestamp; the runtime heartbeat
--                        already drives runtime liveness, but per-task
--                        timestamps let us tell stale tasks apart from
--                        long-running ones in future enhancements.

ALTER TABLE agent_task_queue
  ADD COLUMN attempt INT NOT NULL DEFAULT 1,
  ADD COLUMN max_attempts INT NOT NULL DEFAULT 2,
  ADD COLUMN parent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
  ADD COLUMN failure_reason TEXT,
  ADD COLUMN last_heartbeat_at TIMESTAMPTZ;

CREATE INDEX idx_agent_task_queue_parent ON agent_task_queue(parent_task_id);
