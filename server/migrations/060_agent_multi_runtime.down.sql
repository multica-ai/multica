-- Re-add as nullable first so the UPDATE below can populate it before we
-- apply the NOT NULL constraint.
ALTER TABLE agent ADD COLUMN runtime_id UUID;

-- Restore from the earliest-created assignment row per agent.
UPDATE agent a
SET runtime_id = sub.runtime_id
FROM (
    SELECT DISTINCT ON (agent_id) agent_id, runtime_id
    FROM agent_runtime_assignment
    ORDER BY agent_id, created_at ASC
) sub
WHERE a.id = sub.agent_id;

-- Restore the original invariants: NOT NULL + explicitly named FK that matches
-- what migration 004 originally created (so any further rollback through 004's
-- down migration can drop the constraint by its well-known name).
ALTER TABLE agent ALTER COLUMN runtime_id SET NOT NULL,
    ADD CONSTRAINT agent_runtime_id_fkey
        FOREIGN KEY (runtime_id) REFERENCES agent_runtime(id) ON DELETE RESTRICT;

DROP INDEX IF EXISTS idx_agent_runtime_assignment_runtime;
DROP TABLE agent_runtime_assignment;
