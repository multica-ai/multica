-- Transactional, owner-scoped provider-plan admission for explicitly opted-in
-- agents. Capacity belongs to the human account that owns the paid provider
-- plans, not to one workspace, so the primary key intentionally omits
-- workspace_id. No foreign keys/cascades: owners, runtimes, and task routes are
-- resolved by the application using the same repository convention as the
-- provider-failover ledger.
CREATE TABLE IF NOT EXISTS provider_plan_capacity (
    owner_id UUID NOT NULL,
    provider TEXT NOT NULL CHECK (btrim(provider) <> ''),
    known BOOLEAN NOT NULL DEFAULT FALSE,
    remaining_permille INTEGER NOT NULL DEFAULT 0
        CHECK (remaining_permille BETWEEN 0 AND 1000),
    reserve_permille INTEGER NOT NULL DEFAULT 200
        CHECK (reserve_permille BETWEEN 0 AND 1000),
    reserved_inflight_permille INTEGER NOT NULL DEFAULT 0
        CHECK (reserved_inflight_permille BETWEEN 0 AND 1000),
    window_ends_at TIMESTAMPTZ,
    observed_at TIMESTAMPTZ NOT NULL,
    source TEXT NOT NULL CHECK (btrim(source) <> ''),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS route_admission_state TEXT NOT NULL DEFAULT 'not_applicable'
        CHECK (route_admission_state IN (
            'not_applicable',
            'pending',
            'shadow',
            'routed',
            'deferred',
            'failed'
        )),
    ADD COLUMN IF NOT EXISTS route_decision JSONB,
    ADD COLUMN IF NOT EXISTS route_runtime_id UUID,
    ADD COLUMN IF NOT EXISTS route_provider TEXT,
    ADD COLUMN IF NOT EXISTS route_model TEXT,
    ADD COLUMN IF NOT EXISTS route_thinking_level TEXT,
    ADD COLUMN IF NOT EXISTS route_service_tier TEXT,
    ADD COLUMN IF NOT EXISTS route_runtime_config JSONB,
    ADD COLUMN IF NOT EXISTS route_custom_args JSONB,
    ADD COLUMN IF NOT EXISTS route_capacity_owner_id UUID,
    ADD COLUMN IF NOT EXISTS route_reserved_permille INTEGER NOT NULL DEFAULT 0
        CHECK (route_reserved_permille BETWEEN 0 AND 1000),
    ADD COLUMN IF NOT EXISTS route_admitted_at TIMESTAMPTZ;

-- Fence an opted-in queued task before a polling daemon can claim it. The
-- application resolves the fence in shadow/active/off mode, and the runtime
-- sweeper recovers rows left pending by a process crash. A due deferred task is
-- re-fenced when it is promoted back to queued so it uses current capacity.
CREATE OR REPLACE FUNCTION mark_adaptive_agent_task_pending()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'queued'
       AND NEW.route_admission_state IN ('not_applicable', 'deferred')
       AND EXISTS (
           SELECT 1
           FROM agent a
           WHERE a.id = NEW.agent_id
             AND COALESCE(
                 a.runtime_config #>> '{adaptive_routing,enabled}',
                 'false'
             ) = 'true'
       )
    THEN
        NEW.route_admission_state := 'pending';
        NEW.route_admitted_at := NULL;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_mark_adaptive_agent_task_pending ON agent_task_queue;
CREATE TRIGGER trg_mark_adaptive_agent_task_pending
BEFORE INSERT OR UPDATE OF status ON agent_task_queue
FOR EACH ROW
EXECUTE FUNCTION mark_adaptive_agent_task_pending();

-- Forecast reservations protect plan headroom only while work is in flight.
-- Provider telemetry remains the source of truth for actual consumption.
CREATE OR REPLACE FUNCTION release_adaptive_route_reservation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.route_reserved_permille > 0
       AND OLD.route_capacity_owner_id IS NOT NULL
       AND OLD.route_provider IS NOT NULL
       AND (
           TG_OP = 'DELETE'
           OR (
               OLD.status NOT IN ('completed', 'failed', 'cancelled')
               AND NEW.status IN ('completed', 'failed', 'cancelled')
           )
       )
    THEN
        UPDATE provider_plan_capacity
        SET reserved_inflight_permille = GREATEST(
                0,
                reserved_inflight_permille - OLD.route_reserved_permille
            ),
            updated_at = now()
        WHERE owner_id = OLD.route_capacity_owner_id
          AND provider = OLD.route_provider;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_release_adaptive_route_reservation_update ON agent_task_queue;
CREATE TRIGGER trg_release_adaptive_route_reservation_update
AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW
EXECUTE FUNCTION release_adaptive_route_reservation();

DROP TRIGGER IF EXISTS trg_release_adaptive_route_reservation_delete ON agent_task_queue;
CREATE TRIGGER trg_release_adaptive_route_reservation_delete
AFTER DELETE ON agent_task_queue
FOR EACH ROW
EXECUTE FUNCTION release_adaptive_route_reservation();
