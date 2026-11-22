UPDATE notification_preference
SET preferences = preferences - 'auto_archive_on_agent_trigger',
    updated_at = now()
WHERE preferences ->> 'auto_archive_prompt' = 'pending'
  AND preferences ->> 'auto_archive_on_agent_trigger' = 'muted';
