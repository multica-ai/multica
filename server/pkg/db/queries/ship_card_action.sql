-- name: InsertShipCardAction :one
-- Records a card action attempt at the moment the user presses the chip.
-- Status starts as "in_progress" so async actions (diagnose_ci_failure,
-- summarize_review_feedback) can flip to succeeded/failed when the agent
-- task completes — synchronous actions (comment, merge) finish in the
-- same request and immediately call CompleteShipCardAction.
INSERT INTO ship_card_action (
    workspace_id, pull_request_id, actor_user_id, action, payload,
    result_status, result_payload, completed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: CompleteShipCardAction :one
-- Closes the audit row with the final status + payload. Idempotent on
-- the row id: re-running with a different status overwrites (the
-- agent-task callback path may legitimately update a row that was
-- previously in_progress).
--
-- completed_at is the caller's decision: synchronous actions pass
-- now() so the timestamp is set; async-spawn actions pass a NULL
-- pgtype.Timestamptz so the row stays "open" until the daemon
-- reports the final outcome and re-runs this query with a real
-- timestamp.
UPDATE ship_card_action SET
    result_status  = $2,
    result_payload = $3,
    completed_at   = $4
WHERE id = $1
RETURNING *;

-- name: ListShipCardActionsForPR :many
-- Newest-first audit timeline for a single card. Limited at the SQL
-- layer so a busy PR doesn't stream thousands of rows when the UI
-- expects to render the last few.
SELECT * FROM ship_card_action
WHERE pull_request_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: CreateShipCardActionTask :one
-- Phase 3 — task spawned by the diagnose_ci_failure or
-- summarize_review_feedback chips. No issue_id, no chat_session_id; the
-- daemon detects the variant via context.type == "ship_card_action" and
-- the embedded action subtype tells the prompt template what to do.
-- Mirrors the QuickCreate / ChannelMention task pattern so the scheduler
-- doesn't need a new dispatch path.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, context
) VALUES ($1, $2, NULL, 'queued', $3, $4)
RETURNING *;
