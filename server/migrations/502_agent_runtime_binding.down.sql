-- Retain runtime pools on application rollback; the legacy agent.runtime_id
-- projection still describes the priority-0 runtime, so dropping this table
-- loses only the fallback ordering, which cannot be reconstructed.
DROP TABLE IF EXISTS agent_runtime_binding;
