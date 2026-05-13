-- PUL-102 PR1: foundation for event-driven multi-PR cascade autonomy.
-- DDL only — no service consumers in this migration. PR2-PR8 wire it up.
--
-- Adds four cascade columns to issue, plus two new tables that PR4's
-- background worker uses to dedup webhook events (cascade_retrigger) and
-- queue follow-up events when a run is already active (cascade_pending_event).
-- All indexes the dashboard (PR7), reconciliation cron (PR8), and loop guard
-- (PR4) need are created here so subsequent PRs touch one file each.
--
-- See plans://Multica/2026-05-13-pul-102-event-driven-multi-pr-autonomy.md (rev 3)
-- and docs/agent-runtime-map.md for the per-PR breakdown and rationale.

BEGIN;

-- 1. Cascade columns on issue. NULL cascade_state means "not in a cascade" —
--    every existing single-PR issue stays NULL and behaves exactly as today
--    (regression-safe). The CHECK enumerates the four lifecycle states from
--    the A3 schema collapse (approved | paused | loop_guarded | completed),
--    matching the TEXT+CHECK convention used by issue.status, agent.runtime_mode,
--    and member.role. We deliberately do NOT use CREATE TYPE … AS ENUM because
--    adding a new state later would require ALTER TYPE which serializes against
--    every reader; the CHECK constraint is faster to evolve.
ALTER TABLE issue
    ADD COLUMN cascade_state TEXT NULL
        CHECK (cascade_state IS NULL OR cascade_state IN (
            'approved',
            'paused',
            'loop_guarded',
            'completed'
        )),
    -- Set by /plan-and-implement skill (PR5) on first approval. Pairs with
    -- cascade_state='approved' but is kept separate so the timestamp survives
    -- a paused→approved cycle for audit.
    ADD COLUMN cascade_started_at TIMESTAMPTZ NULL,
    -- P1: top-level last-event timestamp. Top-level rather than a JSONB
    -- extraction so the reconciliation cron (PR8) and dashboard (PR7) queries
    -- can hit a regular btree index instead of a GIN scan on cascade_progress.
    -- Updated by PR4's background worker every time it processes an event.
    ADD COLUMN cascade_last_event_at TIMESTAMPTZ NULL,
    -- G1: progress detail. JSONB so the structure can extend without a
    -- migration. Canonical structure mirrors server/internal/cascade.Progress
    -- (PR1 Go value type): { total_prs, current_step, last_pr_number,
    -- last_pr_merged_at, last_event_type }. Plan-amend (G3), atomic init (A4),
    -- and completion detection (G7) all read/write this column.
    ADD COLUMN cascade_progress JSONB NULL;

-- 2. Dashboard + reconciliation-cron index. Partial index keyed on cascade_state
--    so it stays tiny (only issues that ever entered a cascade appear). Order
--    matches the PR7 query pattern: filter by cascade_state, then sort by
--    cascade_last_event_at DESC for "most recently active" cascades first.
CREATE INDEX idx_issue_cascade_active
    ON issue (cascade_state, cascade_last_event_at DESC)
    WHERE cascade_state IS NOT NULL;

-- 3. Secondary chronological index for dashboard "oldest active cascade" sort
--    and audit queries ("what cascades started this week"). Partial to mirror
--    idx_issue_cascade_active sizing.
CREATE INDEX idx_issue_cascade_started
    ON issue (cascade_started_at)
    WHERE cascade_started_at IS NOT NULL;

-- 4. cascade_retrigger: every inbound webhook event lands here. PR2/PR3 write
--    rows on receipt (handler returns 200 < 1s — A6 async-decoupling). PR4's
--    background worker reads `WHERE processed_at IS NULL` FIFO.
--
--    `event_id UUID UNIQUE` is the idempotency contract: GitHub re-deliveries
--    of the same event (same delivery GUID) collide here and the second
--    INSERT no-ops with a clean unique-violation. The GitHub adapter (PR3)
--    is responsible for generating event_id deterministically from the
--    delivery ID — see also T3 chaos recovery, which depends on this being
--    safe to re-enqueue.
--
--    `action` records what the worker decided to do with the event after
--    lookup + filtering: 'spawn' counts toward loop guard, the *_skip values
--    are observability-only. Constrained via CHECK so a future contributor
--    can't typo a new action value into a metric label.
CREATE TABLE cascade_retrigger (
    id BIGSERIAL PRIMARY KEY,
    event_id UUID NOT NULL UNIQUE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    pr_url TEXT NOT NULL,
    pr_number INT NOT NULL,
    head_sha TEXT NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN (
        'ci_failure',
        'pr_merged',
        'pr_review_change',
        'pr_title_edit'
    )),
    fired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ NULL,
    action TEXT NULL CHECK (action IS NULL OR action IN (
        'spawn',
        'loop_guard_skip',
        'dedup_skip',
        'scope_filter_skip',
        'state_mismatch_skip',
        'plan_amended_pause',
        'queued_pending'
    ))
);

-- 5. Loop-guard index (PR4 hot path). Query shape:
--      SELECT count(DISTINCT head_sha) FROM cascade_retrigger
--      WHERE pr_url = $1
--        AND action = 'spawn'
--        AND fired_at > now() - interval '6 hours';
--    The (pr_url, head_sha, fired_at DESC, action) composite serves the
--    DISTINCT-count under the predicate. Index-only scan possible.
CREATE INDEX idx_cascade_retrigger_loop_guard
    ON cascade_retrigger (pr_url, head_sha, fired_at DESC, action);

-- 6. Unprocessed-FIFO index (PR4 worker, T3 startup re-scan). Partial so it
--    contains only pending events — typically empty in steady state, briefly
--    populated under burst. ORDER BY fired_at gives FIFO drain.
CREATE INDEX idx_cascade_retrigger_unprocessed
    ON cascade_retrigger (fired_at)
    WHERE processed_at IS NULL;

-- 7. cascade_pending_event: queue depth 1 per issue (G2). PK on issue_id
--    enforces the depth-1 invariant — multiple events arriving while a run
--    is active collapse to "the most recent one matters". PR4's worker uses
--    INSERT ... ON CONFLICT (issue_id) DO UPDATE to replace; A2 drain hook
--    in PR4 reads + deletes after the active run terminates.
--
--    event_id FKs cascade_retrigger so the trigger_context can always be
--    reconstructed from the original event without duplicating the payload
--    snapshot. ON DELETE CASCADE so a retrigger row deletion (TTL cleanup
--    in PR8) doesn't leave orphan pendings.
CREATE TABLE cascade_pending_event (
    issue_id UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES cascade_retrigger(event_id) ON DELETE CASCADE,
    trigger_context JSONB NOT NULL,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
