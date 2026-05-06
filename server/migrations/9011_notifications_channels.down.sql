-- Reverse the channel-first conversion: collapse the inbox/notifications
-- per-channel maps back into a single string per key.
--   inbox.<key>=on                                  -> "<key>": "inbox"
--   inbox.<key>=off AND notifications.<key>=on      -> "<key>": "notifications"
--   inbox.<key>=off AND notifications.<key>=off     -> "<key>": "off"
-- Mobile / desktop / mail / channels blocks are dropped (no equivalent in
-- the old single-route model).

UPDATE "user" u
SET preferences = jsonb_set(
    u.preferences,
    '{notifications}',
    COALESCE((
        SELECT jsonb_object_agg(key, value)
        FROM (
            SELECT
                e.key,
                CASE
                    WHEN COALESCE((u.preferences->'notifications'->'inbox'->>e.key) = 'on', false)
                        THEN 'inbox'
                    WHEN COALESCE((u.preferences->'notifications'->'notifications'->>e.key) = 'on', false)
                        THEN 'notifications'
                    ELSE 'off'
                END AS value
            FROM jsonb_each(u.preferences->'notifications'->'inbox') e
        ) collapsed
    ), '{}'::jsonb)
)
WHERE u.preferences ? 'notifications'
  AND jsonb_typeof(u.preferences->'notifications'->'inbox') = 'object';
