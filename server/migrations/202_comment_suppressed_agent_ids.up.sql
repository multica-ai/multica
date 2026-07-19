-- Persist transport-level trigger suppression so a comment deferred while an
-- agent task is active keeps the same routing contract during completion
-- reconciliation. This metadata is intentionally not exposed in comment API
-- responses; it only constrains agent wake delivery.
ALTER TABLE comment
ADD COLUMN suppressed_agent_ids UUID[] NOT NULL DEFAULT '{}';
