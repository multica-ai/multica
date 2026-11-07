-- name: ListChatSessionsPage :many
-- Apply visibility and the cursor before fetching message bodies or unread
-- counts. Only the bounded page needs a message preview.
WITH page AS MATERIALIZED (
    SELECT cs.*, COALESCE(cs.pinned_at, '1970-01-01'::timestamptz) AS sort_pin,
           COALESCE(activity.created_at, cs.updated_at) AS sort_activity
    FROM chat_session cs
    LEFT JOIN LATERAL (
        SELECT m.created_at FROM chat_message m
        WHERE m.chat_session_id = cs.id AND m.message_kind != 'channel_command'
        ORDER BY m.created_at DESC LIMIT 1
    ) activity ON true
    WHERE cs.workspace_id = sqlc.arg(workspace_id)
      AND cs.creator_id = sqlc.arg(creator_id)
      AND cs.agent_id = ANY(sqlc.arg(agent_ids)::uuid[])
      AND cs.status = sqlc.arg(status)
      AND (cs.explicitly_created_at IS NOT NULL OR activity.created_at IS NOT NULL)
      AND (NOT sqlc.arg(has_cursor)::boolean OR
           (COALESCE(cs.pinned_at, '1970-01-01'::timestamptz),
            COALESCE(activity.created_at, cs.updated_at), cs.id) <
           (sqlc.arg(cursor_pin)::timestamptz, sqlc.arg(cursor_activity)::timestamptz, sqlc.arg(cursor_id)::uuid))
    ORDER BY sort_pin DESC, sort_activity DESC, cs.id DESC
    LIMIT sqlc.arg(page_limit)
)
SELECT page.*,
       CASE WHEN page.status = 'archived' THEN 0 ELSE
           (SELECT count(*) FROM chat_message m WHERE m.chat_session_id = page.id
            AND m.role = 'assistant' AND m.created_at > page.last_read_at)
       END::int AS unread_count,
       COALESCE(preview.content, '')::text AS last_message_content,
       COALESCE(preview.role, '')::text AS last_message_role,
       preview.created_at AS last_message_at, preview.failure_reason AS last_message_failure_reason,
       COALESCE(preview.message_kind, '')::text AS last_message_kind
FROM page
LEFT JOIN LATERAL (
    SELECT left(m.content, 256) AS content, m.role, m.created_at, m.failure_reason, m.message_kind
    FROM chat_message m
    WHERE m.chat_session_id = page.id AND m.message_kind != 'channel_command'
    ORDER BY m.created_at DESC, m.id DESC LIMIT 1
) preview ON true
ORDER BY page.sort_pin DESC, page.sort_activity DESC, page.id DESC;

-- name: ListChatSessionSummary :many
-- Counts span all pages, but never include message bodies.
SELECT cs.id, cs.status,
       CASE WHEN cs.status = 'archived' THEN 0 ELSE
           (SELECT count(*) FROM chat_message m WHERE m.chat_session_id = cs.id
            AND m.role = 'assistant' AND m.created_at > cs.last_read_at)
       END::int AS unread_count
FROM chat_session cs
WHERE cs.workspace_id = sqlc.arg(workspace_id) AND cs.creator_id = sqlc.arg(creator_id)
  AND cs.agent_id = ANY(sqlc.arg(agent_ids)::uuid[])
  AND (cs.explicitly_created_at IS NOT NULL OR EXISTS (
      SELECT 1 FROM chat_message m WHERE m.chat_session_id = cs.id AND m.message_kind != 'channel_command'
  ));
