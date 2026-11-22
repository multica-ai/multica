-- MUL-9820: the default auto-archive preset now archives when you complete an
-- issue or trigger an agent, rather than on any comment, status advance or
-- reassignment. Keep the one-time prompt rows seeded by migration 718 on their
-- pre-release behavior (completing only) by also muting
-- auto_archive_on_agent_trigger until the member answers. Any value they
-- already stored wins.
UPDATE notification_preference
SET preferences = jsonb_build_object('auto_archive_on_agent_trigger', 'muted') || preferences,
    updated_at = now()
WHERE preferences ->> 'auto_archive_prompt' = 'pending';
