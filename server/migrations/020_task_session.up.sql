-- Add session persistence columns to agent_task_queue.
-- session_id: the Claude Code session ID returned after execution.
-- work_dir: the working directory used during execution.
-- These enable resuming the same Claude Code session across multiple tasks
-- for the same (agent, issue) pair via --resume <session_id>.
ALTER TABLE agent_task_queue ADD COLUMN session_id TEXT;
ALTER TABLE agent_task_queue ADD COLUMN work_dir TEXT;
