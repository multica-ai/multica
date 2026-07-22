-- Remove FIR-3650's derived audience keys while preserving the legacy keys.
WITH channels(channel) AS (
    VALUES ('inbox'), ('notifications'), ('mobile'), ('desktop'), ('mail')
),
notification_blocks AS (
    SELECT
        u.id,
        jsonb_object_agg(
            c.channel,
            COALESCE(
                u.preferences #> ARRAY['notifications', c.channel],
                '{}'::jsonb
            )
                - 'new_comment.assignee' - 'new_comment.follower'
                - 'status_changed.assignee' - 'status_changed.follower'
                - 'agent_comment_no_tag.assignee' - 'agent_comment_no_tag.follower'
                - 'agent_comment_member_tag.assignee' - 'agent_comment_member_tag.follower'
                - 'agent_comment_agent_tag.assignee' - 'agent_comment_agent_tag.follower'
        ) AS channels
    FROM "user" u
    CROSS JOIN channels c
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
