-- Fork-specific: convert per-event single-route preferences to per-channel
-- on/off preferences (channel-first model). Each notification type can land on
-- multiple channels independently (Inbox, Notifications page, Mobile push,
-- Desktop, Mail) instead of picking exactly one destination.
--
-- Old shape:  preferences.notifications = { "<key>": "inbox"|"notifications"|"off" }
-- New shape:  preferences.notifications = {
--               inbox:         { "<key>": "on"|"off" },
--               notifications: { "<key>": "on"|"off" },
--               mobile:        { "<key>": "on"|"off" },
--               desktop:       { "<key>": "on"|"off" },
--               mail:          { "<key>": "on"|"off" },
--               channels: { mobile: {badge,sound}, desktop: {badge,banner,sound}, mail: {digest} }
--             }
--
-- Conversion rules per existing string value:
--   "inbox"         -> inbox.<key>=on,  notifications.<key>=off
--   "notifications" -> inbox.<key>=off, notifications.<key>=on
--   "off"           -> inbox.<key>=off, notifications.<key>=off
-- Mobile/desktop/mail get no entries from migration; the routing resolver
-- falls back to per-channel defaults defined in Go.

UPDATE "user" u
SET preferences = jsonb_set(
    u.preferences,
    '{notifications}',
    jsonb_build_object(
        'inbox',
        COALESCE((
            SELECT jsonb_object_agg(
                e.key,
                CASE WHEN e.value #>> '{}' = 'inbox' THEN 'on' ELSE 'off' END
            )
            FROM jsonb_each(u.preferences->'notifications') e
            WHERE jsonb_typeof(e.value) = 'string'
        ), '{}'::jsonb),
        'notifications',
        COALESCE((
            SELECT jsonb_object_agg(
                e.key,
                CASE WHEN e.value #>> '{}' = 'notifications' THEN 'on' ELSE 'off' END
            )
            FROM jsonb_each(u.preferences->'notifications') e
            WHERE jsonb_typeof(e.value) = 'string'
        ), '{}'::jsonb)
    )
)
WHERE u.preferences ? 'notifications'
  AND EXISTS (
    SELECT 1
    FROM jsonb_each(u.preferences->'notifications') e
    WHERE jsonb_typeof(e.value) = 'string'
  );
