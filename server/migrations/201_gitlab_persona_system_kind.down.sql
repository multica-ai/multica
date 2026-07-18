UPDATE agent
SET kind = 'user', updated_at = now()
WHERE system_key LIKE 'gitlab:%' AND kind = 'system';
