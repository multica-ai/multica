-- FIR-3650: preserve every existing user's effective channel choices before
-- noisy notification types gain separate assignee and follower defaults.
WITH legacy_channel_defaults(channel, choices) AS (
    VALUES
        ('inbox', '{"new_comment":"on","status_changed":"off","agent_comment_no_tag":"off","agent_comment_member_tag":"on","agent_comment_agent_tag":"on"}'::jsonb),
        ('notifications', '{"new_comment":"on","status_changed":"on","agent_comment_no_tag":"on","agent_comment_member_tag":"on","agent_comment_agent_tag":"on"}'::jsonb),
        ('mobile', '{"new_comment":"on","status_changed":"off","agent_comment_no_tag":"off","agent_comment_member_tag":"on","agent_comment_agent_tag":"off"}'::jsonb),
        ('desktop', '{"new_comment":"off","status_changed":"off","agent_comment_no_tag":"off","agent_comment_member_tag":"on","agent_comment_agent_tag":"off"}'::jsonb),
        ('mail', '{"new_comment":"off","status_changed":"off","agent_comment_no_tag":"off","agent_comment_member_tag":"off","agent_comment_agent_tag":"off"}'::jsonb)
),
legacy_defaults AS (
    SELECT channel, choice.key AS notification_type, choice.value AS default_choice
    FROM legacy_channel_defaults
    CROSS JOIN LATERAL jsonb_each_text(choices) AS choice
),
effective_values AS (
    SELECT
        u.id,
        d.channel,
        d.notification_type,
        CASE
            WHEN u.preferences #>> ARRAY['notifications', d.channel, d.notification_type] IN ('on', 'off')
                THEN u.preferences #>> ARRAY['notifications', d.channel, d.notification_type]
            ELSE d.default_choice
        END AS effective_choice
    FROM "user" u
    CROSS JOIN legacy_defaults d
),
split_values AS (
    SELECT
        e.id,
        e.channel,
        jsonb_object_agg(
            e.notification_type || '.assignee',
            to_jsonb(e.effective_choice)
        ) || jsonb_object_agg(
            e.notification_type || '.follower',
            to_jsonb(e.effective_choice)
        ) AS additions
    FROM effective_values e
    GROUP BY e.id, e.channel
),
notification_blocks AS (
    SELECT
        u.id,
        jsonb_object_agg(
            s.channel,
            s.additions || COALESCE(
                u.preferences #> ARRAY['notifications', s.channel],
                '{}'::jsonb
            )
        ) AS channels
    FROM "user" u
    JOIN split_values s ON s.id = u.id
    GROUP BY u.id
)
UPDATE "user" u
SET preferences = jsonb_set(
    COALESCE(u.preferences, '{}'::jsonb),
    '{notifications}',
    COALESCE(u.preferences->'notifications', '{}'::jsonb) || n.channels,
    true
)
FROM notification_blocks n
WHERE n.id = u.id;
