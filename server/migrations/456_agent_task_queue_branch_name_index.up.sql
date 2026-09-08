-- Exact branch → issue lookup for PR auto-linking (HOM-16).
--
-- A Multica run checks out its repo on a deterministic branch (agent/<name>/<task>)
-- and records that branch on its task row (agent_task_queue.branch_name). When the
-- GitHub webhook later mirrors a PR opened from that branch, the auto-link path can
-- resolve the owning issue by an exact branch match instead of scraping
-- PREFIX-NUMBER identifiers out of the title/body/branch. This partial index keeps
-- that lookup cheap; only worktree/run tasks populate branch_name, so the index stays
-- small.
CREATE INDEX IF NOT EXISTS idx_agent_task_queue_branch_name
    ON agent_task_queue(branch_name)
    WHERE branch_name IS NOT NULL;
