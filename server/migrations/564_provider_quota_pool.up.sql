-- A pool represents one provider account's quota window. It is global because
-- one account can back agents in several workspaces. Membership is explicit:
-- model or runtime names do not identify the account that pays for a run.
CREATE TABLE provider_quota_pool (
    id uuid NOT NULL,
    owner_id uuid NOT NULL,
    name text NOT NULL,
    provider_hint text NOT NULL DEFAULT '',
    timezone text NOT NULL DEFAULT 'UTC',
    state text NOT NULL DEFAULT 'open'
        CHECK (state IN ('open', 'held_exact', 'held_date', 'reset_unknown', 'probe_due', 'probing', 'probe_backoff')),
    reset_at timestamptz,
    reset_date date,
    source_task_id uuid,
    observed_at timestamptz,
    probe_started_at timestamptz,
    probe_task_id uuid,
    probe_attempts integer NOT NULL DEFAULT 0 CHECK (probe_attempts >= 0),
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (revision > 0),
    CHECK (reset_at IS NULL OR reset_date IS NULL),
    CHECK (state <> 'held_exact' OR reset_at IS NOT NULL),
    CHECK (state <> 'probe_backoff' OR reset_at IS NOT NULL),
    CHECK (state <> 'held_date' OR reset_date IS NOT NULL),
    CHECK (state <> 'reset_unknown' OR (reset_at IS NULL AND reset_date IS NULL))
);

-- The current single-binding agent has at most one quota pool. A future
-- binding table can move this relation without changing pool identity.
CREATE TABLE provider_quota_pool_agent (
    agent_id uuid NOT NULL,
    pool_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- No raw provider error or credential enters the audit history. The task ID
-- and normalized reason let an operator trace the source through task records.
CREATE TABLE provider_quota_pool_event (
    id uuid NOT NULL,
    pool_id uuid NOT NULL,
    actor_id uuid,
    source_task_id uuid,
    event_type text NOT NULL CHECK (event_type IN ('held', 'extended', 'probe_due', 'probe_started', 'probe_succeeded', 'probe_failed', 'released', 'membership_changed')),
    reason text NOT NULL DEFAULT '',
    old_state text NOT NULL,
    new_state text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
