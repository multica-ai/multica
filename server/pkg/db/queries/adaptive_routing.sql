-- name: LockTaskForAdaptiveAdmission :one
SELECT *
FROM agent_task_queue
WHERE id = $1
  AND status = 'queued'
  AND route_admission_state = 'pending'
FOR UPDATE;

-- name: ListProviderPlanCapacitiesForOwnerProvidersForUpdate :many
SELECT *
FROM provider_plan_capacity
WHERE owner_id = @owner_id
  AND provider = ANY(@providers::text[])
ORDER BY provider
FOR UPDATE;

-- name: ListAdaptiveCandidateRuntimes :many
SELECT *
FROM agent_runtime
WHERE id = ANY(@runtime_ids::uuid[]);

-- name: ResolveTaskAdaptiveAdmission :one
UPDATE agent_task_queue
SET route_admission_state = @route_admission_state,
    route_decision = @route_decision,
    route_admission_attempts = route_admission_attempts + 1,
    route_admitted_at = now()
WHERE id = @id
  AND status = 'queued'
  AND route_admission_state = 'pending'
RETURNING *;

-- name: RouteTaskAdaptiveAdmission :one
UPDATE agent_task_queue
SET runtime_id = @runtime_id,
    route_admission_state = 'routed',
    route_decision = @route_decision,
    route_runtime_id = @runtime_id,
    route_provider = @route_provider,
    route_model = @route_model,
    route_thinking_level = @route_thinking_level,
    route_service_tier = @route_service_tier,
    route_runtime_config = @route_runtime_config,
    route_custom_args = @route_custom_args,
    route_admission_attempts = route_admission_attempts + 1,
    route_capacity_owner_id = @route_capacity_owner_id,
    route_reserved_permille = @route_reserved_permille,
    route_admitted_at = now()
WHERE id = @id
  AND status = 'queued'
  AND route_admission_state = 'pending'
RETURNING *;

-- name: DeferTaskAdaptiveAdmission :one
UPDATE agent_task_queue
SET status = 'deferred',
    fire_at = now() + make_interval(secs => @retry_after_secs::double precision),
    route_admission_state = 'deferred',
    route_decision = @route_decision,
    route_admission_attempts = route_admission_attempts + 1,
    route_admitted_at = now()
WHERE id = @id
  AND status = 'queued'
  AND route_admission_state = 'pending'
RETURNING *;

-- name: FailTaskAdaptiveAdmission :one
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = @error,
    failure_reason = 'adaptive_routing_admission_failed',
    fire_at = NULL,
    prepare_lease_expires_at = NULL,
    route_admission_state = 'failed',
    route_decision = @route_decision,
    route_admission_attempts = route_admission_attempts + 1,
    route_admitted_at = now()
WHERE id = @id
  AND status = 'queued'
  AND route_admission_state = 'pending'
RETURNING *;

-- name: ReserveProviderPlanCapacity :one
UPDATE provider_plan_capacity
SET reserved_inflight_permille =
        reserved_inflight_permille + @reserve_permille::int,
    updated_at = now()
WHERE owner_id = @owner_id
  AND provider = @provider
  AND known
  AND reserved_inflight_permille + @reserve_permille::int
      <= GREATEST(remaining_permille - reserve_permille, 0)
RETURNING *;

-- name: ListPendingAdaptiveAdmissions :many
SELECT *
FROM agent_task_queue
WHERE status = 'queued'
  AND route_admission_state = 'pending'
  AND created_at <= now() - make_interval(secs => @min_age_secs::double precision)
ORDER BY created_at ASC, id ASC
LIMIT @max_rows::int;

-- name: PromoteDueAdaptiveRoutingTasks :many
WITH due AS (
    SELECT id
    FROM agent_task_queue
    WHERE status = 'deferred'
      AND route_admission_state = 'deferred'
      AND fire_at <= now()
    ORDER BY fire_at ASC, id ASC
    LIMIT @max_rows::int
    FOR UPDATE SKIP LOCKED
)
UPDATE agent_task_queue task
SET status = 'queued'
FROM due
WHERE task.id = due.id
  AND task.status = 'deferred'
  AND task.route_admission_state = 'deferred'
RETURNING task.*;

-- name: UpsertProviderPlanCapacity :one
INSERT INTO provider_plan_capacity (
    owner_id,
    provider,
    known,
    remaining_permille,
    reserve_permille,
    window_ends_at,
    observed_at,
    source
)
VALUES (
    @owner_id,
    @provider,
    @known,
    @remaining_permille,
    @reserve_permille,
    @window_ends_at,
    @observed_at,
    @source
)
ON CONFLICT (owner_id, provider)
DO UPDATE SET
    known = EXCLUDED.known,
    remaining_permille = EXCLUDED.remaining_permille,
    reserve_permille = EXCLUDED.reserve_permille,
    window_ends_at = EXCLUDED.window_ends_at,
    observed_at = EXCLUDED.observed_at,
    source = EXCLUDED.source,
    updated_at = now()
RETURNING *;
