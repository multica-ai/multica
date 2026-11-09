-- name: ExpireChannelIssueNotices :exec
UPDATE channel_issue_notice SET status = 'failed', owner_token = NULL, updated_at = now()
WHERE status IN ('queued', 'sending') AND retry_at <= now()
  AND (attempts >= 3 OR first_attempt_at < now() - interval '4 minutes');

-- name: ClaimChannelIssueNotice :one
UPDATE channel_issue_notice
SET status = 'sending', attempts = attempts + 1, owner_token = sqlc.arg(owner_token),
    first_attempt_at = COALESCE(first_attempt_at, now()), retry_at = now() + interval '45 seconds', updated_at = now()
WHERE id = (
    SELECT id FROM channel_issue_notice WHERE status IN ('queued', 'sending') AND retry_at <= now()
      AND attempts < 3 AND (first_attempt_at IS NULL OR first_attempt_at >= now() - interval '4 minutes')
    ORDER BY retry_at LIMIT 1 FOR UPDATE SKIP LOCKED
) RETURNING *;

-- name: FinishChannelIssueNotice :execrows
UPDATE channel_issue_notice
SET status = sqlc.arg(status), message_id = NULLIF(sqlc.arg(message_id)::text, ''),
    owner_token = NULL, retry_at = now() + make_interval(secs => sqlc.arg(retry_seconds)::double precision), updated_at = now()
WHERE id = sqlc.arg(id) AND owner_token = sqlc.arg(owner_token) AND status = 'sending';

-- name: GetChannelIssueNoticeRoute :one
SELECT ci.id AS installation_id, ci.workspace_id, ci.agent_id, ci.channel_type, ci.config,
    ci.status, ci.installer_user_id, ci.ws_lease_token, ci.ws_lease_expires_at, ci.installed_at, ci.created_at, ci.updated_at,
    i.title, i.number, w.slug, w.issue_prefix, a.name AS agent_name
FROM channel_issue_notice n
JOIN issue i ON i.id = n.issue_id AND i.workspace_id = n.workspace_id AND i.parent_issue_id IS NULL
JOIN workspace w ON w.id = n.workspace_id
JOIN agent_task_queue t ON t.id = i.origin_id AND t.agent_id = i.creator_id
JOIN channel_task_delivery d ON d.task_id = n.source_task_id AND d.task_id = COALESCE(t.chat_input_task_id, t.id)
    AND d.installation_id = n.installation_id AND d.binding_id = n.binding_id
    AND d.channel_type = 'feishu' AND d.chat_type = 'group'
    AND d.channel_chat_id = n.channel_chat_id AND d.channel_sender_id = n.channel_sender_id
    AND d.config = n.route_config
JOIN channel_installation ci ON ci.id = n.installation_id AND ci.workspace_id = n.workspace_id
    AND ci.agent_id = t.agent_id AND ci.channel_type = 'feishu' AND ci.status = 'active'
    AND ci.installed_at <= d.created_at
    AND COALESCE(ci.config->>'binding_generation', '') = n.binding_generation
JOIN agent a ON a.id = ci.agent_id AND a.workspace_id = n.workspace_id AND a.archived_at IS NULL
JOIN chat_session cs ON cs.id = t.chat_session_id AND cs.workspace_id = n.workspace_id AND cs.status = 'active'
JOIN channel_chat_session_binding cb ON cb.id = n.binding_id AND cb.installation_id = ci.id
    AND cb.chat_session_id = cs.id AND cb.channel_chat_id = n.channel_chat_id
    AND cb.channel_type = 'feishu' AND cb.chat_type = 'group'
JOIN channel_user_binding ub ON ub.installation_id = ci.id AND ub.workspace_id = n.workspace_id
    AND ub.multica_user_id = n.recipient_user_id AND ub.channel_user_id = n.channel_sender_id AND ub.channel_type = 'feishu'
JOIN member m ON m.workspace_id = n.workspace_id AND m.user_id = n.recipient_user_id
WHERE n.id = sqlc.arg(id) AND n.owner_token = sqlc.arg(owner_token) AND n.status = 'sending'
  AND i.origin_type = 'agent_create' AND i.creator_type = 'agent' AND t.originator_user_id = n.recipient_user_id
  AND (i.status = n.status_key OR (n.kind = 'completed' AND (i.status = 'done' OR EXISTS (
      SELECT 1 FROM issue_status s WHERE s.workspace_id = i.workspace_id AND s.key = i.status AND s.category = 'done'
  ))))
  AND NOT EXISTS (SELECT 1 FROM channel_issue_notice newer WHERE newer.issue_id = n.issue_id AND newer.issue_revision > n.issue_revision)
  AND (n.failed_task_id IS NULL OR NOT EXISTS (
      SELECT 1 FROM agent_task_queue newer JOIN agent_task_queue failed ON failed.id = n.failed_task_id
      WHERE newer.issue_id = i.id AND (newer.created_at, newer.id) > (failed.created_at, failed.id)
  ));
