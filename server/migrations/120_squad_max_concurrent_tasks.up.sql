ALTER TABLE squad ADD COLUMN max_concurrent_tasks INTEGER NOT NULL DEFAULT 0;
COMMENT ON COLUMN squad.max_concurrent_tasks IS 'Maximum concurrent tasks for this squad. 0 means no limit (same semantics as agent.max_concurrent_tasks).';
