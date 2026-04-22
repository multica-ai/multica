-- Migration: 046_agent_unique_name
-- Enforces uniqueness of agent names within a workspace so that the API can
-- return a clear 409 Conflict instead of a silent duplicate or a 500 error.
--
-- Step 1 deduplicates any existing rows that would violate the constraint,
-- keeping the most recently updated agent when names collide.
-- Step 2 adds the constraint so future inserts are rejected at the DB level.
--
-- See: docs/improvements.md PR-3, docs/pr-strategy.md Milestone 1

-- Step 1: delete duplicates, keep the most recently updated one
DELETE FROM agent a
USING (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY workspace_id, name ORDER BY updated_at DESC) AS rn
    FROM agent
) ranked
WHERE a.id = ranked.id AND ranked.rn > 1;

-- Step 2: add the constraint
ALTER TABLE agent
    ADD CONSTRAINT agent_workspace_name_unique UNIQUE (workspace_id, name);
