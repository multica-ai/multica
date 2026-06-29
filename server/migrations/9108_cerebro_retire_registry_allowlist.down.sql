-- Rollback for 9108_cerebro_retire_registry_allowlist.
--
-- Removes only the rows inserted by the up migration. Rows with the same key
-- that were already in place (ON CONFLICT DO NOTHING in up) are untouched.
-- Identifying the migrated rows by the tuple (tool_key='firtal_registry',
-- layer='agent', resource_pattern='') is precise enough: before the migration
-- no such rows existed (the registry used the allowlist, not the chain).

DELETE FROM cerebro_tool_policy
WHERE tool_key         = 'firtal_registry'
  AND layer            = 'agent'
  AND resource_pattern = ''
  AND subject_id IN (
      SELECT atg.agent_id
      FROM agent_tool_grant atg
      WHERE atg.tool_name  = 'firtal_registry'
        AND atg.enabled    = true
        AND atg.config_json IS NOT NULL
  );
