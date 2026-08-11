UPDATE agent_runtime
SET runtime_mode = 'cloud',
    updated_at = now()
WHERE provider = 'platform-agent-cli'
  AND profile_id IS NULL
  AND runtime_mode <> 'cloud';
