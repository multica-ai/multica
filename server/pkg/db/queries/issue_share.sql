-- name: GetIssueShareToken :one
SELECT token FROM issue_share WHERE issue_id = $1 AND workspace_id = $2;

-- name: CreateIssueShare :one
INSERT INTO issue_share (issue_id, workspace_id, token, created_by)
SELECT i.id, i.workspace_id, sqlc.arg(token), sqlc.arg(user_id)
FROM issue i
JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = sqlc.arg(user_id)
WHERE i.id = sqlc.arg(issue_id) AND i.workspace_id = sqlc.arg(workspace_id)
  AND i.project_id IS NOT DISTINCT FROM sqlc.narg(project_id)::uuid
  AND (
      m.role IN ('owner', 'admin')
      OR (m.role = 'member' AND (
          (i.creator_type = 'member' AND i.creator_id = m.user_id)
          OR (i.assignee_type = 'member' AND i.assignee_id = m.user_id)
      ))
  )
FOR SHARE OF i
ON CONFLICT (issue_id) DO UPDATE SET token = issue_share.token
WHERE issue_share.workspace_id = EXCLUDED.workspace_id
RETURNING token;

-- name: RevokeIssueShare :execrows
DELETE FROM issue_share s
USING issue i, member m
WHERE s.issue_id = sqlc.arg(issue_id) AND s.workspace_id = sqlc.arg(workspace_id)
  AND s.token = sqlc.arg(token)
  AND i.id = s.issue_id AND i.workspace_id = s.workspace_id
  AND m.workspace_id = s.workspace_id AND m.user_id = sqlc.arg(user_id)
  AND (
      m.role IN ('owner', 'admin')
      OR (m.role = 'member' AND (
          (i.creator_type = 'member' AND i.creator_id = m.user_id)
          OR (i.assignee_type = 'member' AND i.assignee_id = m.user_id)
      ))
  );

-- name: GetSharedIssue :one
WITH shared AS (
    SELECT i.*
    FROM issue_share s
    JOIN issue i ON i.id = s.issue_id AND i.workspace_id = s.workspace_id
    JOIN "user" viewer ON viewer.id = sqlc.arg(viewer_id)
    WHERE s.token = sqlc.arg(token)
), shared_comments AS (
    SELECT c.*
    FROM shared i
    JOIN comment c ON c.issue_id = i.id AND c.workspace_id = i.workspace_id
    WHERE c.deleted_at IS NULL AND c.quick_action_id IS NULL
      AND c.author_type IN ('member', 'agent') AND c.type IN ('comment', 'progress_update')
      AND (c.source_task_id IS NULL OR EXISTS (
          SELECT 1 FROM agent_task_queue source
          WHERE source.id = c.source_task_id AND source.issue_id = i.id
            AND source.chat_session_id IS NULL
      ))
), shared_runs AS (
    SELECT r.id, r.status, r.started_at, r.completed_at, r.agent_id
    FROM shared i
    JOIN agent_task_queue r ON r.issue_id = i.id
    WHERE r.chat_session_id IS NULL
      AND r.status IN ('completed', 'failed', 'cancelled')
      AND r.completed_at IS NOT NULL
      AND NOT (r.escalation_for_task_id IS NOT NULL AND r.started_at IS NULL AND r.status = 'cancelled')
), shared_agents AS (
    SELECT a.id, a.name, a.avatar_url
    FROM shared i
    JOIN agent a ON a.workspace_id = i.workspace_id
    JOIN agent_invocation_target target ON target.agent_id = a.id
      AND target.target_type = 'workspace' AND target.target_id = i.workspace_id
    WHERE a.permission_mode = 'public_to'
)
SELECT jsonb_build_object(
    'identifier', w.issue_prefix || '-' || i.number::text,
    'title', i.title,
    'description', i.description,
    'status', i.status,
    'status_label', COALESCE(st.name, ''),
    'comments', COALESCE((
        SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
            'id', c.id, 'author_type', c.author_type,
            'content', c.content, 'created_at', c.created_at,
            'parent_id', parent.id, 'source_task_id', run.id,
            'author_name', COALESCE(u.name, a.name),
            'author_avatar_url', COALESCE(u.avatar_url, a.avatar_url)
        )) ORDER BY c.created_at, c.id)
        FROM shared_comments c
        LEFT JOIN shared_comments parent ON parent.id = c.parent_id
        LEFT JOIN shared_runs run ON run.id = c.source_task_id
        LEFT JOIN "user" u ON c.author_type = 'member' AND u.id = c.author_id
        LEFT JOIN shared_agents a ON c.author_type = 'agent' AND a.id = c.author_id
    ), '[]'::jsonb),
    'runs', COALESCE((
        SELECT jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
            'id', r.id, 'status', r.status, 'started_at', r.started_at, 'completed_at', r.completed_at,
            'agent_name', a.name, 'agent_avatar_url', a.avatar_url
        )) ORDER BY r.completed_at DESC, r.id)
        FROM shared_runs r
        LEFT JOIN shared_agents a ON a.id = r.agent_id
    ), '[]'::jsonb)
)::jsonb AS shared_issue
FROM shared i
JOIN workspace w ON w.id = i.workspace_id
LEFT JOIN issue_status st ON st.workspace_id = i.workspace_id AND st.key = i.status;

-- name: ListSharedIssueRunMessages :many
SELECT COALESCE(page.seq, -1)::integer AS seq,
       COALESCE(page.type, '')::text AS type,
       COALESCE(page.content, '')::text AS content,
       page.created_at
FROM issue_share s
JOIN issue i ON i.id = s.issue_id AND i.workspace_id = s.workspace_id
JOIN "user" viewer ON viewer.id = sqlc.arg(viewer_id)
JOIN agent_task_queue r ON r.issue_id = i.id AND r.id = sqlc.arg(run_id)
LEFT JOIN LATERAL (
    SELECT m.seq, m.type, m.content, m.created_at
    FROM task_message m
    WHERE m.task_id = r.id AND m.seq > sqlc.arg(after_seq)::integer
      AND m.type IN ('text', 'text_delta', 'commentary') AND m.content IS NOT NULL
    ORDER BY m.seq
    LIMIT sqlc.arg(page_size)::integer
) page ON true
WHERE s.token = sqlc.arg(token) AND r.chat_session_id IS NULL
  AND r.status IN ('completed', 'failed', 'cancelled') AND r.completed_at IS NOT NULL
  AND NOT (r.escalation_for_task_id IS NOT NULL AND r.started_at IS NULL AND r.status = 'cancelled')
ORDER BY page.seq;

-- name: ListSharedIssueDeliverables :many
SELECT latest.*
FROM (
    SELECT DISTINCT ON (btrim(lower(split_part(a.content_type, ';', 1))), btrim(a.filename)) a.*
    FROM issue_share s
    JOIN issue i ON i.id = s.issue_id AND i.workspace_id = s.workspace_id
    JOIN "user" viewer ON viewer.id = sqlc.arg(viewer_id)
    JOIN comment c ON c.issue_id = i.id AND c.workspace_id = i.workspace_id
    JOIN attachment a ON a.comment_id = c.id AND a.workspace_id = i.workspace_id
      AND (a.issue_id IS NULL OR a.issue_id = i.id)
    WHERE s.token = sqlc.arg(token)
      AND c.deleted_at IS NULL AND c.quick_action_id IS NULL AND c.type = 'comment'
      AND c.author_type IN ('member', 'agent')
      AND (c.source_task_id IS NULL OR EXISTS (
          SELECT 1 FROM agent_task_queue source
          WHERE source.id = c.source_task_id AND source.issue_id = i.id
            AND source.chat_session_id IS NULL
      ))
      AND (a.task_id IS NULL OR EXISTS (
          SELECT 1 FROM agent_task_queue source
          WHERE source.id = a.task_id AND source.issue_id = i.id
            AND source.chat_session_id IS NULL
      ))
      AND a.chat_session_id IS NULL AND a.chat_message_id IS NULL AND a.source_context_id IS NULL
      AND lower(btrim(a.filename)) <> 'askuser.json'
    ORDER BY btrim(lower(split_part(a.content_type, ';', 1))), btrim(a.filename), a.created_at DESC, a.id DESC
) latest
WHERE (sqlc.narg(attachment_id)::uuid IS NULL OR latest.id = sqlc.narg(attachment_id)::uuid)
ORDER BY latest.created_at DESC, latest.id DESC;
