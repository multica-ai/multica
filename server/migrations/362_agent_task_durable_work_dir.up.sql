-- Records the durable directory that replaces a disposable task worktree
-- after the daemon has confirmed that worktree was finalized and removed.
-- NULL means no such handoff was reported, so clients must keep using work_dir.
ALTER TABLE agent_task_queue
ADD COLUMN IF NOT EXISTS durable_work_dir TEXT;
