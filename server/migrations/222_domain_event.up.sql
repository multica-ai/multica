-- Event Hooks MVP — PR1: the transactional-outbox domain event log (MUL-4332 §4.1).
--
-- `domain_event` is the persisted source of truth for the future hooks engine.
-- Every domain command that commits a fact (issue created / status changed /
-- assigned, comment created, task completed / failed) will ALSO insert one row
-- here IN THE SAME TRANSACTION, so the fact and its event commit atomically —
-- the classic transactional outbox. A crash after the domain write but before
-- any downstream reaction can therefore never lose the event.
--
-- PR1 ships the table + the write-path convergence only. There is NO consumer
-- yet: rows land with dispatch_status='pending' and nothing reads them, so this
-- is a zero-behavior-change, additive migration. The matcher/executor that
-- claims pending rows via the lease columns arrives in PR3.
--
-- The in-memory `events.Bus` (internal/events) is SEPARATE and unchanged — it
-- keeps serving realtime UI fanout. This table is the durable, transactional
-- fact log; the Bus is best-effort post-commit push. They do not replace each
-- other.
--
-- Workspace DB rules (CLAUDE.md + MUL-4332 §4): NO foreign key, NO cascade —
-- every UUID association is validated in the application layer. Secondary and
-- unique indexes are added in their own single-statement CONCURRENTLY
-- migrations (223–227), never inline, so index builds never take an ACCESS
-- EXCLUSIVE lock during deploy.

-- Monotonic dispatch / drain boundary. `seq` orders events for stable scanning
-- and the future issue-scope drain boundary; it is NOT a promise of global
-- business ordering across all actions (MUL-4332 §4.1). A sequence (not a
-- serial/identity column) so the ownership is explicit and the down migration
-- can drop it cleanly.
CREATE SEQUENCE IF NOT EXISTS domain_event_seq;

CREATE TABLE IF NOT EXISTS domain_event (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seq                    BIGINT NOT NULL DEFAULT nextval('domain_event_seq'),

    -- Fact envelope.
    workspace_id           UUID NOT NULL,
    type                   TEXT NOT NULL,          -- e.g. issue.status_changed
    schema_version         INT  NOT NULL,          -- per-type payload schema version
    subject_type           TEXT NOT NULL,          -- issue | comment | task
    subject_id             UUID NOT NULL,
    actor_type             TEXT NOT NULL,          -- member | agent | system | hook
    actor_id               UUID,                   -- NULL for system actors
    payload                JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Causal chain. A human root event has correlation_id = id and hop_count = 0;
    -- events produced by a future hook action inherit the correlation, record the
    -- producing execution/action, and increment hop_count (guardrail in PR3).
    correlation_id         UUID NOT NULL,
    causation_execution_id UUID,
    causation_action_index INT,
    hop_count              INT NOT NULL DEFAULT 0,

    -- Single-consumer outbox lease. Unused in PR1 (no matcher); rows stay
    -- 'pending'. PR3's matcher claims via lease_token/lease_expires_at and
    -- advances dispatch_status.
    dispatch_status        TEXT NOT NULL DEFAULT 'pending'
                               CHECK (dispatch_status IN ('pending', 'dispatching', 'dispatched', 'failed')),
    attempts               INT NOT NULL DEFAULT 0,
    available_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token            UUID,
    lease_expires_at       TIMESTAMPTZ,
    dispatched_at          TIMESTAMPTZ,

    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tie the sequence lifecycle to the column so a table drop reclaims it.
ALTER SEQUENCE domain_event_seq OWNED BY domain_event.seq;

-- Application-layer integrity only: proactively drop any *_fkey a tool might
-- add, matching the workspace no-FK / no-cascade rule (see 186_autopilot_rule_version).
ALTER TABLE domain_event
    DROP CONSTRAINT IF EXISTS domain_event_workspace_id_fkey;

COMMENT ON TABLE domain_event IS
    'Transactional-outbox domain event log (MUL-4332 §4.1). One row per committed domain fact, written in the same tx as the fact. Source of truth for the hooks engine; no FK, no cascade, app-layer integrity. dispatch_* columns are the single-consumer outbox lease consumed by the PR3 matcher.';
