-- Workspace deletion is application-owned. These statements form a fixed,
-- bottom-up deletion plan; the legacy foreign keys remain only as a
-- compatibility safety net during the expand phase.

-- name: SetWorkspaceTeardownMode :exec
-- The setting is transaction-local. Migration 242 makes only the three DELETE
-- dirty triggers skip their per-row work while this flag is on.
SELECT set_config('multica.workspace_teardown', 'on', true);

-- name: LockTaskUsageRollupForWorkspaceDelete :exec
-- Advisory lock 4246 is the hourly rollup family's key: the rollup function
-- takes it per transaction (migration 272) and the backfill commands take it
-- per session. An in-flight rollup finishes first, then no new rollup can
-- write the workspace's aggregates until this delete commits. The caller
-- bounds this wait with SET LOCAL lock_timeout — batch jobs hold 4246 for
-- minutes, and an unbounded wait here hangs the delete request (MUL-5983).
SELECT pg_advisory_xact_lock(4246);

-- name: LockWorkspaceTaskOwnerAgents :exec
-- The three LockWorkspaceTaskOwner* statements are a write fence, not a lookup —
-- they deliberately return nothing, so a workspace with a huge owner set costs
-- the API process no memory at all.
--
-- agent_task_queue has no workspace_id, so a task becomes this workspace's
-- problem through agent_id, issue_id or runtime_id — and inserting such a task
-- requires FOR KEY SHARE on the referenced owner row, which conflicts with
-- FOR UPDATE. Holding these locks for the rest of the teardown transaction
-- therefore blocks any enqueue (or reassignment) that would add a task to this
-- workspace after we have swept it, which row locks on the tasks alone cannot
-- do. Same technique the handler already uses on the workspace row to keep
-- CreateChatSession out of the delete window.
--
-- This fence is defence in depth, not the only guarantee: deleteWorkspaceTasks
-- re-verifies every owner after sweeping it and fails closed if tasks keep
-- appearing, so teardown does not silently leak rows even if the fence is ever
-- weakened (for example by removing the legacy FKs this one leans on).
SELECT 1 FROM agent WHERE agent.workspace_id = $1 FOR UPDATE;

-- name: LockWorkspaceTaskOwnerIssues :exec
SELECT 1 FROM issue WHERE issue.workspace_id = $1 FOR UPDATE;

-- name: LockWorkspaceTaskOwnerRuntimes :exec
SELECT 1 FROM agent_runtime WHERE agent_runtime.workspace_id = $1 FOR UPDATE;

-- name: ListWorkspaceAgentIDFirstPage :many
-- First page of the same walk. Split from the keyset query rather than seeded
-- with the all-zero uuid, because that value is itself a valid uuid: a row whose
-- id happened to be all zeros would be skipped forever by `id > $2`.
SELECT id FROM agent
WHERE agent.workspace_id = $1
ORDER BY id
LIMIT $2;

-- name: ListWorkspaceAgentIDPage :many
-- Owner enumeration in bounded, keyset-paged chunks. The rows are already locked
-- by LockWorkspaceTaskOwnerAgents above; this only walks them.
--
-- Keyset on id, not on a natural key: the cursor column must be immutable, and
-- renaming an agent mid-teardown would otherwise let it sort behind the cursor
-- and escape the sweep. Uses idx_agent_workspace_id_keyset (migration 281), which
-- exists so this page does not sort the whole owner set.
SELECT id FROM agent
WHERE agent.workspace_id = $1 AND id > $2
ORDER BY id
LIMIT $3;

-- name: ListWorkspaceIssueIDFirstPage :many
-- First page of the same walk. Split from the keyset query rather than seeded
-- with the all-zero uuid, because that value is itself a valid uuid: a row whose
-- id happened to be all zeros would be skipped forever by `id > $2`.
SELECT id FROM issue
WHERE issue.workspace_id = $1
ORDER BY id
LIMIT $2;

-- name: ListWorkspaceIssueIDPage :many
-- Uses idx_issue_workspace_id_keyset (migration 282).
SELECT id FROM issue
WHERE issue.workspace_id = $1 AND id > $2
ORDER BY id
LIMIT $3;

-- name: ListWorkspaceRuntimeIDFirstPage :many
-- First page of the same walk. Split from the keyset query rather than seeded
-- with the all-zero uuid, because that value is itself a valid uuid: a row whose
-- id happened to be all zeros would be skipped forever by `id > $2`.
SELECT id FROM agent_runtime
WHERE agent_runtime.workspace_id = $1
ORDER BY id
LIMIT $2;

-- name: ListWorkspaceRuntimeIDPage :many
-- Uses idx_agent_runtime_workspace_id_keyset (migration 283).
SELECT id FROM agent_runtime
WHERE agent_runtime.workspace_id = $1 AND id > $2
ORDER BY id
LIMIT $3;

-- name: ListTaskIDsByAgentFirstPage :many
-- First page of the same walk; see ListWorkspaceAgentIDFirstPage for why this is
-- a separate query rather than a zero-uuid cursor, and ListTaskIDsByAgentPage for
-- the single-key / keyset / FOR UPDATE rationale.
SELECT id FROM agent_task_queue
WHERE agent_id = $1
ORDER BY id
LIMIT $2
FOR UPDATE;

-- name: ListTaskIDsByAgentPage :many
-- One owner key per call, one keyset-paged chunk per call.
--
-- Single-key equality is deliberate. All three ownership paths must stay (nothing
-- enforces that a task's agent/runtime/issue share a workspace), but expressing
-- them as `OR agent_id IN (subquery) OR …` — or as a UNION of joins, or as
-- `= ANY($1::uuid[])` over a whole workspace's agents — makes the planner
-- estimate a proportional share of the global table and fall back to a Seq Scan
-- on agent_task_queue. A single-key equality is the only form whose row estimate
-- stays small enough to be index-driven regardless of how the workspace's tasks
-- are distributed (MUL-5999).
--
-- `id > $2 ORDER BY id LIMIT $3` against idx_agent_task_queue_agent_id_keyset
-- (migration 278) bounds the SCAN, not just the result: each page is an index
-- range scan that starts where the previous one stopped. Bounding only the result
-- — `ORDER BY id LIMIT n` against the (agent_id, status) index — puts a Sort over
-- the agent's entire task set in front of every page, so a busy agent costs
-- roughly O(N² / page) for the sweep.
--
-- FOR UPDATE is load-bearing for correctness, not throughput. It locks the rows
-- this page is about to delete, and under READ COMMITTED PostgreSQL re-checks the
-- WHERE clause after acquiring each lock: a task a concurrent committed UPDATE
-- has already moved to another owner no longer matches and is skipped, and one we
-- did lock cannot be moved away before we delete it.
SELECT id FROM agent_task_queue
WHERE agent_id = $1 AND id > $2
ORDER BY id
LIMIT $3
FOR UPDATE;

-- name: ListTaskIDsByIssueFirstPage :many
-- First page of the same walk; see ListWorkspaceAgentIDFirstPage for why this is
-- a separate query rather than a zero-uuid cursor, and ListTaskIDsByAgentPage for
-- the single-key / keyset / FOR UPDATE rationale.
SELECT id FROM agent_task_queue
WHERE issue_id = $1
ORDER BY id
LIMIT $2
FOR UPDATE;

-- name: ListTaskIDsByIssuePage :many
-- Uses idx_agent_task_queue_issue_id_keyset (migration 279). See
-- ListTaskIDsByAgentPage for the single-key / keyset / FOR UPDATE rationale.
SELECT id FROM agent_task_queue
WHERE issue_id = $1 AND id > $2
ORDER BY id
LIMIT $3
FOR UPDATE;

-- name: ListTaskIDsByRuntimeFirstPage :many
-- First page of the same walk; see ListWorkspaceAgentIDFirstPage for why this is
-- a separate query rather than a zero-uuid cursor, and ListTaskIDsByAgentPage for
-- the single-key / keyset / FOR UPDATE rationale.
SELECT id FROM agent_task_queue
WHERE runtime_id = $1
ORDER BY id
LIMIT $2
FOR UPDATE;

-- name: ListTaskIDsByRuntimePage :many
-- Uses idx_agent_task_queue_runtime_id (migration 273, (runtime_id, id)); before
-- it every runtime_id index was partial, so this path had none at all. See
-- ListTaskIDsByAgentPage for the single-key / keyset / FOR UPDATE rationale.
SELECT id FROM agent_task_queue
WHERE runtime_id = $1 AND id > $2
ORDER BY id
LIMIT $3
FOR UPDATE;

-- name: DetachTaskBatchReferences :exec
-- Break inbound references to a task batch before deleting it, in the
-- application layer rather than through the FKs' ON DELETE SET NULL. Both
-- referencing columns are indexed (idx_agent_task_queue_parent from migration
-- 055, idx_autopilot_run_task_id from migration 277) so this stays a per-batch
-- index scan instead of the per-row full scan the FK action would do.
--
-- Kept separate from DeleteTaskBatch: a task can be both a member of the batch
-- and the parent of another member, and updating plus deleting the same row
-- inside one statement is not well defined.
WITH detached_runs AS (
    UPDATE autopilot_run
    SET task_id = NULL
    WHERE task_id = ANY(@task_ids::uuid[])
)
UPDATE agent_task_queue
SET parent_task_id = NULL
WHERE parent_task_id = ANY(@task_ids::uuid[]);

-- name: DeleteTaskBatch :exec
-- Deletes one bounded batch of tasks together with everything that hangs off
-- them, every arm keyed by task_id against an existing index, then the task rows
-- by primary key. The legacy FK cascades stay a safety net only: this statement
-- is what actually removes the rows, so teardown keeps working when they go.
WITH
batch AS MATERIALIZED (
    SELECT id FROM unnest(@task_ids::uuid[]) AS t(id)
),
deleted_task_usage AS (
    DELETE FROM task_usage WHERE task_id IN (SELECT id FROM batch)
),
deleted_task_messages AS (
    DELETE FROM task_message WHERE task_id IN (SELECT id FROM batch)
),
deleted_task_tokens AS (
    DELETE FROM task_token WHERE task_id IN (SELECT id FROM batch)
),
deleted_channel_outbound_cards AS (
    DELETE FROM channel_outbound_card_message WHERE task_id IN (SELECT id FROM batch)
),
deleted_lark_outbound_cards AS (
    DELETE FROM lark_outbound_card_message WHERE task_id IN (SELECT id FROM batch)
),
deleted_draft_restores AS (
    DELETE FROM chat_draft_restore WHERE task_id IN (SELECT id FROM batch)
)
DELETE FROM agent_task_queue WHERE id IN (SELECT id FROM batch);

-- name: DeleteTaskTokensByAgent :exec
-- The third explicit task_token path. workspace_id (in DeleteWorkspaceLeafData)
-- and task_id (in DeleteTaskBatch) do not cover a token whose own workspace_id
-- points at a neighbour while its agent lives here, and teardown must not fall
-- back on the agent_id cascade for it. Uses idx_task_token_agent_id
-- (migration 275); one agent key per call for the same planner reason as
-- ListTaskIDsByAgentBatch.
DELETE FROM task_token WHERE agent_id = $1;

-- name: PrepareWorkspaceDeletionLinks :exec
-- Break self-references and the task/run cycle explicitly before deleting
-- either side. This keeps their ordering in the application-owned graph
-- instead of relying on ON DELETE SET NULL actions.
--
-- The task half of this step now lives in DetachTaskBatchReferences, which runs
-- once per bounded batch instead of once for a whole workspace's task set.
WITH
detached_comments AS (
    UPDATE comment
    SET parent_id = NULL
    WHERE comment.workspace_id = $1
      AND parent_id IS NOT NULL
),
detached_issues AS (
    UPDATE issue
    SET parent_issue_id = NULL
    WHERE issue.workspace_id = $1
      AND parent_issue_id IS NOT NULL
)
UPDATE webhook_delivery
SET replayed_from_delivery_id = NULL
WHERE webhook_delivery.workspace_id = $1
  AND replayed_from_delivery_id IS NOT NULL;

-- name: DeleteWorkspaceLeafData :exec
-- Everything task-keyed moved to DeleteTaskBatch, which runs in bounded batches
-- before this step; what is left is keyed by the workspace or by one of the
-- workspace-scoped id sets below.
WITH
ws_agents AS MATERIALIZED (
    SELECT id FROM agent WHERE workspace_id = $1
),
ws_issues AS MATERIALIZED (
    SELECT id FROM issue WHERE workspace_id = $1
),
ws_labels AS MATERIALIZED (
    SELECT id FROM issue_label WHERE workspace_id = $1
),
ws_skills AS MATERIALIZED (
    SELECT id FROM skill WHERE workspace_id = $1
),
ws_squads AS MATERIALIZED (
    SELECT id FROM squad WHERE workspace_id = $1
),
ws_sessions AS MATERIALIZED (
    SELECT id FROM chat_session WHERE workspace_id = $1
),
ws_autopilots AS MATERIALIZED (
    SELECT id FROM autopilot WHERE workspace_id = $1
),
ws_github_prs AS MATERIALIZED (
    SELECT id FROM github_pull_request WHERE workspace_id = $1
),
ws_vcs_prs AS MATERIALIZED (
    SELECT id FROM vcs_pull_request WHERE workspace_id = $1
),
ws_vcs_connections AS MATERIALIZED (
    SELECT id FROM vcs_connection WHERE workspace_id = $1
),
ws_channel_installations AS MATERIALIZED (
    SELECT id FROM channel_installation WHERE workspace_id = $1
),
ws_lark_installations AS MATERIALIZED (
    SELECT id FROM lark_installation WHERE workspace_id = $1
),
-- One of three explicit task_token paths. This is the workspace-keyed one;
-- DeleteTaskBatch covers task_id and DeleteTaskTokensByAgent covers agent_id, so
-- a token whose workspace_id points at a neighbour while its task or agent lives
-- here is still removed by this teardown rather than by the FK cascade. The
-- former single statement combined all three with OR, which cost a full scan of
-- task_token (MUL-5999); split, each path is an index scan.
deleted_task_tokens AS (
    DELETE FROM task_token
    WHERE workspace_id = $1
),
deleted_hourly_dirty AS (
    DELETE FROM task_usage_hourly_dirty WHERE workspace_id = $1
),
deleted_hourly AS (
    DELETE FROM task_usage_hourly WHERE workspace_id = $1
),
deleted_attachments AS (
    DELETE FROM attachment WHERE workspace_id = $1
),
deleted_channel_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT id FROM ws_sessions)
),
deleted_lark_outbound_cards AS (
    DELETE FROM lark_outbound_card_message
    WHERE chat_session_id IN (SELECT id FROM ws_sessions)
),
deleted_draft_restores AS (
    DELETE FROM chat_draft_restore
    WHERE chat_session_id IN (SELECT id FROM ws_sessions)
),
-- Same no-FK chore as chat_draft_restore above. Matched on workspace_id rather
-- than the session set because that column exists precisely so this statement
-- does not have to join through chat_session, which it deletes in this same CTE.
deleted_agent_builder_drafts AS (
    DELETE FROM agent_builder_draft WHERE workspace_id = $1
),
deleted_comment_reactions AS (
    DELETE FROM comment_reaction WHERE workspace_id = $1
),
deleted_issue_reactions AS (
    DELETE FROM issue_reaction WHERE workspace_id = $1
),
deleted_activity AS (
    DELETE FROM activity_log WHERE workspace_id = $1
),
deleted_inbox AS (
    DELETE FROM inbox_item WHERE workspace_id = $1
),
deleted_issue_dependencies AS (
    DELETE FROM issue_dependency
    WHERE issue_id IN (SELECT id FROM ws_issues)
       OR depends_on_issue_id IN (SELECT id FROM ws_issues)
),
deleted_issue_subscribers AS (
    DELETE FROM issue_subscriber
    WHERE issue_id IN (SELECT id FROM ws_issues)
),
deleted_issue_labels AS (
    DELETE FROM issue_to_label
    WHERE issue_id IN (SELECT id FROM ws_issues)
       OR label_id IN (SELECT id FROM ws_labels)
),
deleted_agent_labels AS (
    DELETE FROM agent_to_label
    WHERE agent_id IN (SELECT id FROM ws_agents)
       OR label_id IN (SELECT id FROM ws_labels)
),
deleted_skill_labels AS (
    DELETE FROM skill_to_label
    WHERE skill_id IN (SELECT id FROM ws_skills)
       OR label_id IN (SELECT id FROM ws_labels)
),
deleted_issue_github_links AS (
    DELETE FROM issue_pull_request
    WHERE issue_id IN (SELECT id FROM ws_issues)
       OR pull_request_id IN (SELECT id FROM ws_github_prs)
),
deleted_issue_vcs_links AS (
    DELETE FROM issue_vcs_pull_request
    WHERE issue_id IN (SELECT id FROM ws_issues)
       OR pull_request_id IN (SELECT id FROM ws_vcs_prs)
),
deleted_agent_invocation_targets AS (
    DELETE FROM agent_invocation_target
    WHERE agent_id IN (SELECT id FROM ws_agents)
),
deleted_agent_skills AS (
    DELETE FROM agent_skill
    WHERE agent_id IN (SELECT id FROM ws_agents)
       OR skill_id IN (SELECT id FROM ws_skills)
),
deleted_skill_files AS (
    DELETE FROM skill_file
    WHERE skill_id IN (SELECT id FROM ws_skills)
),
deleted_daemon_connections AS (
    DELETE FROM daemon_connection
    WHERE agent_id IN (SELECT id FROM ws_agents)
),
deleted_squad_members AS (
    DELETE FROM squad_member
    WHERE squad_id IN (SELECT id FROM ws_squads)
),
deleted_project_resources AS (
    DELETE FROM project_resource WHERE workspace_id = $1
),
deleted_autopilot_collaborators AS (
    DELETE FROM autopilot_collaborator
    WHERE autopilot_id IN (SELECT id FROM ws_autopilots)
),
deleted_autopilot_subscribers AS (
    DELETE FROM autopilot_subscriber
    WHERE autopilot_id IN (SELECT id FROM ws_autopilots)
),
deleted_webhook_deliveries AS (
    DELETE FROM webhook_delivery WHERE workspace_id = $1
),
deleted_github_check_runs AS (
    DELETE FROM github_pull_request_check_run
    WHERE pr_id IN (SELECT id FROM ws_github_prs)
),
deleted_github_check_suites AS (
    DELETE FROM github_pull_request_check_suite
    WHERE pr_id IN (SELECT id FROM ws_github_prs)
),
deleted_pending_github_suites AS (
    DELETE FROM github_pending_check_suite WHERE workspace_id = $1
),
deleted_vcs_commit_statuses AS (
    DELETE FROM vcs_commit_status
    WHERE connection_id IN (SELECT id FROM ws_vcs_connections)
),
deleted_channel_chat_bindings AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM ws_channel_installations)
       OR chat_session_id IN (SELECT id FROM ws_sessions)
),
deleted_channel_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM ws_channel_installations)
),
deleted_channel_inbound_audit AS (
    DELETE FROM channel_inbound_audit
    WHERE installation_id IN (SELECT id FROM ws_channel_installations)
),
deleted_channel_user_bindings AS (
    DELETE FROM channel_user_binding WHERE workspace_id = $1
),
deleted_channel_binding_tokens AS (
    DELETE FROM channel_binding_token WHERE workspace_id = $1
),
deleted_lark_chat_bindings AS (
    DELETE FROM lark_chat_session_binding
    WHERE installation_id IN (SELECT id FROM ws_lark_installations)
       OR chat_session_id IN (SELECT id FROM ws_sessions)
),
deleted_lark_inbound_dedup AS (
    DELETE FROM lark_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM ws_lark_installations)
),
deleted_lark_inbound_audit AS (
    DELETE FROM lark_inbound_audit
    WHERE installation_id IN (SELECT id FROM ws_lark_installations)
),
deleted_lark_user_bindings AS (
    DELETE FROM lark_user_binding WHERE workspace_id = $1
),
deleted_lark_binding_tokens AS (
    DELETE FROM lark_binding_token WHERE workspace_id = $1
)
-- Keep the two-system cleanup ledger until object storage has been settled.
-- Moving every row out of pending also prevents a concurrent media bind from
-- attaching an object after the workspace teardown commits. The reconciler
-- performs the idempotent object delete and clears the row afterwards.
UPDATE channel_media_pending_object
SET state = CASE
        WHEN state = 'tombstoned' THEN 'tombstoned'
        ELSE 'deleting'
    END,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now(),
    last_error = NULL
WHERE channel_media_pending_object.workspace_id = $1;

-- name: DeleteWorkspaceChatMessages :exec
DELETE FROM chat_message
WHERE chat_session_id IN (
    SELECT id FROM chat_session WHERE chat_session.workspace_id = $1
);

-- name: DeleteWorkspaceCommunicationRoots :exec
WITH
deleted_sessions AS (
    DELETE FROM chat_session WHERE chat_session.workspace_id = $1
),
deleted_channel_installations AS (
    DELETE FROM channel_installation
    WHERE channel_installation.workspace_id = $1
)
DELETE FROM lark_installation WHERE lark_installation.workspace_id = $1;

-- name: DeleteWorkspaceComments :exec
DELETE FROM comment WHERE comment.workspace_id = $1;

-- name: DeleteWorkspaceIssueRoots :exec
WITH
deleted_issues AS (
    DELETE FROM issue WHERE issue.workspace_id = $1
),
deleted_labels AS (
    DELETE FROM issue_label WHERE issue_label.workspace_id = $1
),
deleted_properties AS (
    DELETE FROM issue_property WHERE issue_property.workspace_id = $1
),
deleted_issue_views AS (
    DELETE FROM issue_view WHERE issue_view.workspace_id = $1
),
deleted_issue_view_preferences AS (
    DELETE FROM issue_view_preference
    WHERE issue_view_preference.workspace_id = $1
)
DELETE FROM quick_action WHERE quick_action.workspace_id = $1;

-- name: DeleteWorkspaceAutopilotRuns :exec
DELETE FROM autopilot_run
WHERE autopilot_id IN (
    SELECT id FROM autopilot WHERE autopilot.workspace_id = $1
);

-- name: DeleteWorkspaceAutopilotChildren :exec
WITH
deleted_triggers AS (
    DELETE FROM autopilot_trigger
    WHERE autopilot_id IN (
        SELECT id FROM autopilot WHERE autopilot.workspace_id = $1
    )
)
DELETE FROM autopilot_rule_version
WHERE autopilot_rule_version.workspace_id = $1;

-- name: DeleteWorkspaceAutopilots :exec
DELETE FROM autopilot WHERE autopilot.workspace_id = $1;

-- name: DeleteWorkspacePullRequests :exec
WITH deleted_github_prs AS (
    DELETE FROM github_pull_request
    WHERE github_pull_request.workspace_id = $1
)
DELETE FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = $1;

-- name: DeleteWorkspaceConnections :exec
WITH deleted_github_installations AS (
    DELETE FROM github_installation
    WHERE github_installation.workspace_id = $1
)
DELETE FROM vcs_connection WHERE vcs_connection.workspace_id = $1;

-- name: DeleteWorkspaceSquadsAndSkills :exec
WITH deleted_squads AS (
    DELETE FROM squad WHERE squad.workspace_id = $1
)
DELETE FROM skill WHERE skill.workspace_id = $1;

-- name: DeleteWorkspacePluginData :exec
-- Plugin relationships have no foreign keys or cascades. Delete the append-only
-- grant/binding history first, then installation rows. Global identity, release,
-- contribution, and artifact rows survive for other workspaces and historical
-- execution-manifest attribution.
WITH installations AS MATERIALIZED (
    SELECT plugin_installation.id
    FROM plugin_installation
    WHERE plugin_installation.workspace_id = $1
),
deleted_health AS (
    DELETE FROM plugin_health
    WHERE workspace_id = $1
),
deleted_execution_manifests AS (
    DELETE FROM plugin_execution_manifest
    WHERE workspace_id = $1
),
deleted_snapshots AS (
    DELETE FROM plugin_capability_snapshot
    WHERE workspace_id = $1
),
deleted_workspace_state AS (
    DELETE FROM plugin_workspace_capability_state
    WHERE workspace_id = $1
),
deleted_bindings AS (
    DELETE FROM plugin_binding
    WHERE installation_id IN (SELECT id FROM installations)
),
deleted_grants AS (
    DELETE FROM plugin_grant
    WHERE installation_id IN (SELECT id FROM installations)
)
DELETE FROM plugin_installation WHERE id IN (SELECT id FROM installations);

-- name: DeleteWorkspaceAgents :exec
DELETE FROM agent WHERE agent.workspace_id = $1;

-- name: DeleteWorkspaceRuntimesAndProjects :exec
WITH
deleted_runtimes AS (
    DELETE FROM agent_runtime WHERE agent_runtime.workspace_id = $1
),
deleted_profiles AS (
    DELETE FROM runtime_profile WHERE runtime_profile.workspace_id = $1
)
DELETE FROM project WHERE project.workspace_id = $1;

-- name: DeleteWorkspaceAdministration :exec
WITH
deleted_members AS (
    DELETE FROM member WHERE member.workspace_id = $1
),
deleted_notification_preferences AS (
    DELETE FROM notification_preference
    WHERE notification_preference.workspace_id = $1
),
deleted_pins AS (
    DELETE FROM pinned_item WHERE pinned_item.workspace_id = $1
),
deleted_daemon_tokens AS (
    DELETE FROM daemon_token WHERE daemon_token.workspace_id = $1
),
detached_feedback AS (
    UPDATE feedback
    SET workspace_id = NULL
    WHERE feedback.workspace_id = $1
),
detached_client_usage AS (
    UPDATE client_usage_daily
    SET workspace_id = NULL
    WHERE client_usage_daily.workspace_id = $1
)
DELETE FROM workspace_invitation
WHERE workspace_invitation.workspace_id = $1;
