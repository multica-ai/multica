DROP TRIGGER IF EXISTS trg_release_adaptive_route_reservation_delete ON agent_task_queue;
DROP TRIGGER IF EXISTS trg_release_adaptive_route_reservation_update ON agent_task_queue;
DROP FUNCTION IF EXISTS release_adaptive_route_reservation();

DROP TRIGGER IF EXISTS trg_mark_adaptive_agent_task_pending ON agent_task_queue;
DROP FUNCTION IF EXISTS mark_adaptive_agent_task_pending();

-- A rollback must not leave queued work pinned to a runtime chosen by code the
-- downgraded server can no longer explain. Restore only unstarted work to the
-- source agent's durable binding; already-running/terminal history is retained.
UPDATE agent_task_queue atq
SET runtime_id = a.runtime_id
FROM agent a
WHERE atq.agent_id = a.id
  AND atq.route_admission_state = 'routed'
  AND atq.status IN ('queued', 'deferred');

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS route_admitted_at,
    DROP COLUMN IF EXISTS route_reserved_permille,
    DROP COLUMN IF EXISTS route_capacity_owner_id,
    DROP COLUMN IF EXISTS route_custom_args,
    DROP COLUMN IF EXISTS route_runtime_config,
    DROP COLUMN IF EXISTS route_service_tier,
    DROP COLUMN IF EXISTS route_thinking_level,
    DROP COLUMN IF EXISTS route_model,
    DROP COLUMN IF EXISTS route_provider,
    DROP COLUMN IF EXISTS route_runtime_id,
    DROP COLUMN IF EXISTS route_decision,
    DROP COLUMN IF EXISTS route_admission_state;

DROP TABLE IF EXISTS provider_plan_capacity;
