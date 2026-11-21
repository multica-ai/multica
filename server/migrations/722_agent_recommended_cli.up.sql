-- Smart runtime: the recommendation is a CLI (cursor/codex/...), not a
-- destination machine. Persist the provider name on the agent row and drop
-- the leftover recommended runtime_id binding so ownership is not stored.
ALTER TABLE agent
    ADD COLUMN recommended_cli TEXT NOT NULL DEFAULT '';

UPDATE agent AS a
SET recommended_cli = r.provider
FROM agent_runtime AS r
WHERE a.share_runtime = false
  AND a.runtime_id IS NOT NULL
  AND a.runtime_id = r.id
  AND a.recommended_cli = '';

UPDATE agent
SET runtime_id = NULL
WHERE share_runtime = false
  AND runtime_id IS NOT NULL;
