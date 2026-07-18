-- GitLab comment-attribution personas are identity shells, not workers.
-- Flip existing rows to kind=system so they drop out of GetAgentInWorkspace
-- (assign/edit) while ListAgents still returns them via the gitlab:% filter.
UPDATE agent
SET kind = 'system', updated_at = now()
WHERE system_key LIKE 'gitlab:%' AND kind = 'user';
