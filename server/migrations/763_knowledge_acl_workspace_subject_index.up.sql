-- Recomputing an agent's access reads every grant that names the agent's squads,
-- floors or the agent, inside one workspace.
CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_acl_workspace_subject_idx
    ON knowledge_acl (workspace_id, subject_type, subject_id);
