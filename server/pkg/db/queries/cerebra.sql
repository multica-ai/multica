-- name: GetRuntimeTierModelMap :one
-- Returns the tier_model_map JSONB for one runtime.
-- NULL result means routing is not configured for this runtime.
SELECT tier_model_map
FROM   agent_runtime
WHERE  id = $1;

-- name: SetRuntimeTierModelMap :exec
-- Upserts the tier_model_map for a runtime.
UPDATE agent_runtime
SET    tier_model_map = $2
WHERE  id = $1;

-- name: GetAllRuntimeTierModelMaps :many
-- Returns all runtimes that have a tier_model_map configured.
-- Used by the daemon at startup to pre-load the routing table.
SELECT id, tier_model_map
FROM   agent_runtime
WHERE  tier_model_map IS NOT NULL;

-- ─────────────────────────────────────────────────────────────────────────────
-- Session model (issue)
-- ─────────────────────────────────────────────────────────────────────────────

-- name: GetIssueSessionModel :one
SELECT session_model, session_model_updated_at
FROM   issue
WHERE  id = $1;

-- name: SetIssueSessionModel :exec
UPDATE issue
SET    session_model            = $2,
       session_model_updated_at = NOW()
WHERE  id = $1;

-- name: ClearIssueSessionModel :exec
UPDATE issue
SET    session_model            = NULL,
       session_model_updated_at = NULL
WHERE  id = $1;

-- ─────────────────────────────────────────────────────────────────────────────
-- Session model (chat_session)
-- ─────────────────────────────────────────────────────────────────────────────

-- name: GetChatSessionModel :one
SELECT session_model, session_model_updated_at
FROM   chat_session
WHERE  id = $1;

-- name: SetChatSessionModel :exec
UPDATE chat_session
SET    session_model            = $2,
       session_model_updated_at = NOW()
WHERE  id = $1;

-- name: ClearChatSessionModel :exec
UPDATE chat_session
SET    session_model            = NULL,
       session_model_updated_at = NULL
WHERE  id = $1;

-- ─────────────────────────────────────────────────────────────────────────────
-- Model unavailability
-- ─────────────────────────────────────────────────────────────────────────────

-- name: UpsertModelUnavailability :exec
INSERT INTO cerebra_model_unavailability (runtime_id, model, marked_at, ttl_seconds)
VALUES ($1, $2, NOW(), $3)
ON CONFLICT (runtime_id, model)
    DO UPDATE SET marked_at   = NOW(),
                  ttl_seconds = EXCLUDED.ttl_seconds;

-- name: GetModelUnavailability :one
SELECT runtime_id, model, marked_at, ttl_seconds
FROM   cerebra_model_unavailability
WHERE  runtime_id = $1
  AND  model      = $2;

-- name: DeleteExpiredModelUnavailability :exec
-- Run periodically to purge expired rows.
DELETE FROM cerebra_model_unavailability
WHERE  marked_at + (ttl_seconds * INTERVAL '1 second') < NOW();

-- name: DeleteModelUnavailability :exec
DELETE FROM cerebra_model_unavailability
WHERE  runtime_id = $1
  AND  model      = $2;

-- ─────────────────────────────────────────────────────────────────────────────
-- Routing log
-- ─────────────────────────────────────────────────────────────────────────────

-- name: InsertRoutingLog :exec
INSERT INTO cerebra_routing_log (
    id, task_id, issue_id, session_id, runtime_id,
    chosen_model, tier, matched_rule, tool_chain_expected,
    fallback_used, latency_ms, status,
    policy_reason, candidate_count, classifier_version,
    estimated_cost, input_tokens, output_tokens
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12,
    $13, $14, $15,
    $16, $17, $18
);

-- name: GetRoutingLogsByTask :many
SELECT *
FROM   cerebra_routing_log
WHERE  task_id = $1
ORDER BY created_at DESC;

-- name: GetRoutingLogsByIssue :many
SELECT *
FROM   cerebra_routing_log
WHERE  issue_id = $1
ORDER BY created_at DESC
LIMIT  50;
