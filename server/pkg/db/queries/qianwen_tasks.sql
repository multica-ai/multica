-- Qianwen current-task discovery projection.
--
-- This query intentionally does not select agent_task_queue.result, error,
-- context, work_dir, chat messages, or opaque Qianwen identity values. It
-- resolves the signed caller, applies the product's member VIEW rules, and
-- adds a creator-only fence for chat tasks before producing the small shape
-- spoken by a private Skill.

-- name: ListQianwenVisibleCurrentTasks :many
WITH RECURSIVE caller AS MATERIALIZED (
    SELECT
        installation.id AS installation_id,
        installation.workspace_id,
        installation.agent_id AS installed_agent_id,
        binding.multica_user_id,
        membership.role
    FROM channel_installation AS installation
    JOIN channel_user_binding AS binding
      ON binding.installation_id = installation.id
     AND binding.workspace_id = installation.workspace_id
     AND binding.channel_type = 'qianwen'
    JOIN member AS membership
      ON membership.workspace_id = installation.workspace_id
     AND membership.user_id = binding.multica_user_id
    JOIN member AS installer_membership
      ON installer_membership.workspace_id = installation.workspace_id
     AND installer_membership.user_id = installation.installer_user_id
    JOIN agent AS installed_agent
      ON installed_agent.id = installation.agent_id
     AND installed_agent.workspace_id = installation.workspace_id
     AND installed_agent.kind = 'user'
     AND installed_agent.archived_at IS NULL
    WHERE installation.id = sqlc.arg('installation_id')::uuid
      AND installation.channel_type = 'qianwen'
      AND installation.status = 'active'
      AND installation.config ->> 'mode' = 'personal_polling'
      AND installation.config ->> 'app_id' = sqlc.arg('connection_id')::text
      AND installation.config ->> 'access_token_hash' = sqlc.arg('access_token_hash')::text
      AND binding.multica_user_id = sqlc.arg('multica_user_id')::uuid
      AND binding.channel_user_id = sqlc.arg('open_user_id')::text
      AND binding.config ->> 'open_uuid' = sqlc.arg('open_uuid')::text
      AND binding.config ->> 'identity_scope' = 'skill'
      AND (
            installed_agent.owner_id = binding.multica_user_id
         OR (
                installed_agent.permission_mode = 'public_to'
            AND EXISTS (
                SELECT 1
                FROM agent_invocation_target AS target
                WHERE target.agent_id = installed_agent.id
                  AND (
                        (target.target_type = 'workspace' AND target.target_id = installation.workspace_id)
                     OR (target.target_type = 'member' AND target.target_id = binding.multica_user_id)
                  )
            )
         )
      )
), visible_agents AS MATERIALIZED (
    SELECT candidate.id, candidate.name
    FROM agent AS candidate
    JOIN caller ON caller.workspace_id = candidate.workspace_id
    WHERE candidate.kind = 'user'
      AND candidate.archived_at IS NULL
      AND (
            caller.role IN ('owner', 'admin')
         OR candidate.owner_id = caller.multica_user_id
         OR (
                candidate.permission_mode = 'public_to'
            AND EXISTS (
                SELECT 1
                FROM agent_invocation_target AS target
                WHERE target.agent_id = candidate.id
                  AND (
                        (target.target_type = 'workspace' AND target.target_id = caller.workspace_id)
                     OR (target.target_type = 'member' AND target.target_id = caller.multica_user_id)
                  )
            )
         )
      )
), request_tasks AS (
    SELECT
        ledger.request_id,
        root.id AS task_id,
        ledger.chat_session_id,
        ARRAY[root.id]::uuid[] AS path
    FROM qianwen_skill_request AS ledger
    JOIN caller
      ON caller.installation_id = ledger.installation_id
     AND caller.multica_user_id = ledger.multica_user_id
    JOIN agent_task_queue AS root
      ON root.id = ledger.task_id
     AND root.agent_id = caller.installed_agent_id
     AND root.chat_session_id = ledger.chat_session_id
     AND root.initiator_user_id = caller.multica_user_id
     AND root.originator_user_id = caller.multica_user_id
     AND root.accountable_user_id = caller.multica_user_id
     AND root.regenerate_quick_actions_for IS NULL
    WHERE ledger.task_id IS NOT NULL

    UNION ALL

    SELECT
        parent.request_id,
        child.id,
        parent.chat_session_id,
        parent.path || ARRAY[child.id]::uuid[]
    FROM request_tasks AS parent
    JOIN caller ON true
    JOIN agent_task_queue AS child
      ON child.retry_of_task_id = parent.task_id
     AND child.agent_id = caller.installed_agent_id
     AND child.chat_session_id = parent.chat_session_id
     AND child.originator_user_id = caller.multica_user_id
     AND child.accountable_user_id = caller.multica_user_id
     AND (child.initiator_user_id IS NULL OR child.initiator_user_id = caller.multica_user_id)
     AND child.regenerate_quick_actions_for IS NULL
    WHERE NOT (child.id = ANY(parent.path))
), request_lookup AS MATERIALIZED (
    SELECT DISTINCT ON (task_id) task_id, request_id
    FROM request_tasks
    ORDER BY task_id, request_id
), page AS MATERIALIZED (
    SELECT
        task.id AS task_id,
        request_lookup.request_id,
        CASE
            WHEN linked_issue.id IS NOT NULL THEN 'issue'
            WHEN owned_chat.id IS NOT NULL THEN 'chat'
            WHEN task.autopilot_run_id IS NOT NULL THEN 'autopilot'
            WHEN task.context ->> 'type' = 'quick_create' THEN 'quick_create'
            ELSE 'task'
        END::text AS source,
        CASE
            WHEN linked_issue.id IS NOT NULL THEN linked_issue.title
            WHEN owned_chat.id IS NOT NULL THEN COALESCE(NULLIF(btrim(owned_chat.title), ''), 'Chat task')
            WHEN task.autopilot_run_id IS NOT NULL THEN COALESCE(NULLIF(btrim(task.trigger_summary), ''), 'Autopilot task')
            WHEN task.context ->> 'type' = 'quick_create' THEN 'Creating an issue'
            ELSE visible_agents.name || ' task ' || left(task.id::text, 8)
        END::text AS display_title,
        visible_agents.name AS agent_name,
        task.status AS task_status,
        task.created_at,
        task.started_at
    FROM agent_task_queue AS task
    JOIN visible_agents ON visible_agents.id = task.agent_id
    JOIN caller ON true
    LEFT JOIN issue AS linked_issue
      ON linked_issue.id = task.issue_id
     AND linked_issue.workspace_id = caller.workspace_id
    LEFT JOIN chat_session AS owned_chat
      ON owned_chat.id = task.chat_session_id
     AND owned_chat.workspace_id = caller.workspace_id
     AND owned_chat.agent_id = task.agent_id
     AND owned_chat.creator_id = caller.multica_user_id
    LEFT JOIN request_lookup ON request_lookup.task_id = task.id
    WHERE task.status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
      AND task.regenerate_quick_actions_for IS NULL
      AND (task.issue_id IS NULL OR linked_issue.id IS NOT NULL)
      AND (task.chat_session_id IS NULL OR owned_chat.id IS NOT NULL)
      AND (
            NOT sqlc.arg('cursor_valid')::boolean
         OR (task.created_at, task.id) < (
                sqlc.narg('cursor_created_at')::timestamptz,
                sqlc.narg('cursor_id')::uuid
            )
      )
    ORDER BY task.created_at DESC, task.id DESC
    LIMIT sqlc.arg('page_size')::int
)
SELECT
    (page.task_id IS NOT NULL)::boolean AS has_task,
    page.task_id,
    page.request_id,
    page.display_title,
    page.source,
    page.agent_name,
    page.task_status,
    page.created_at,
    page.started_at
FROM caller
LEFT JOIN page ON true
ORDER BY page.created_at DESC NULLS LAST, page.task_id DESC NULLS LAST;
