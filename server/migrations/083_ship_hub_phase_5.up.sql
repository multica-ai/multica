-- Ship Hub Phase 5 — the "hand-holding" phase. Three new persistence
-- surfaces land together because they're tightly coupled to one another
-- and to the existing pull_request / deploy tables:
--
--  1. risk_level + risk_reasons on pull_request
--     Phase 1 derived a "touches schema / migration" hint from a keyword
--     scan on the title and labels (deriveRiskHint in
--     packages/views/ship/hooks/use-pr-state.ts). That heuristic was a
--     placeholder; Phase 5 replaces it with a four-tier rule-based
--     classifier whose verdict is recorded on the row so the same answer
--     is shown on every Kanban load (no per-render recomputation, no
--     drift between web and desktop).
--
--  2. deploy_preflight
--     Pre-production safety checklist. One row per (environment, sha) so
--     the same SHA targeted at different environments has independent
--     checklists. UNIQUE on the same pair makes the get-or-create
--     endpoint idempotent — re-opening the dialog hits the existing row.
--
--  3. deploy_health_snapshot
--     Periodic (5min) post-deploy health probe. Append-only audit so the
--     "In Production" card can render an error/latency Δ panel and
--     surface a "Rollback?" chip when thresholds trip. The snapshot
--     comparison happens in the goroutine that writes the rows; the
--     handler reads the latest few back for the live panel.
--
-- Naming + indexing follows the pattern set by 079..082: the columns
-- the Kanban filters on (workspace_id, state) get partial indexes; the
-- per-row read patterns (deploy id timeline) get descending order.

-- 1. Risk classification --------------------------------------------------

-- Four tiers — `low | medium | high | critical`. We keep the enum
-- because the UI maps values 1:1 to colors and it's far cheaper to
-- migrate a 4-element enum than to validate a free-text column on
-- every read. New tiers (e.g. "blocker") would require a CREATE TYPE
-- migration; that's the right friction.
CREATE TYPE risk_level AS ENUM ('low', 'medium', 'high', 'critical');

ALTER TABLE pull_request
    ADD COLUMN risk_level         risk_level NOT NULL DEFAULT 'medium',
    -- Free-form list of trigger strings ("migration file detected",
    -- "auth handler changed", etc.). Render verbatim in the "Why this
    -- risk?" popover. JSONB rather than text[] so we can later attach
    -- per-trigger metadata (file path, severity weight) without a
    -- column rename.
    ADD COLUMN risk_reasons        JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- When the classifier last ran. NULL means "not yet classified" —
    -- the reconciler treats those rows as backfill candidates.
    ADD COLUMN risk_classified_at  TIMESTAMPTZ;

-- Workspace-scoped open-PR list pivoting on risk for the "high-risk
-- review queue" surface and the ambient sidebar's "failing" segment.
-- Partial because closed/merged PRs aren't actionable in this view.
CREATE INDEX idx_pull_request_risk
    ON pull_request(workspace_id, risk_level)
    WHERE state = 'open';

-- 2. Pre-flight production gate ------------------------------------------

CREATE TABLE deploy_preflight (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    environment_id  UUID NOT NULL REFERENCES deploy_environment(id) ON DELETE CASCADE,
    -- We key on (env, sha) rather than on a deploy row id because the
    -- pre-flight checklist exists BEFORE the deploy is created. The
    -- promotion endpoint creates the deploy row only after
    -- promote_check_pass returns true.
    target_sha      TEXT NOT NULL,
    -- Boolean toggles the user clicks on the checklist. We deliberately
    -- don't model these as a single JSONB column — separate columns
    -- give us cheap partial indexes ("which envs are awaiting QA?")
    -- and unambiguous typings on the wire.
    migrations_ok   BOOLEAN NOT NULL DEFAULT FALSE,
    smoke_tests_ok  BOOLEAN NOT NULL DEFAULT FALSE,
    -- QA verification is a who+when stamp (vs a boolean) so the audit
    -- trail can answer "who QA'd this build" without a separate
    -- comments table.
    qa_verified_at  TIMESTAMPTZ,
    qa_verified_by  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    -- Free-text description of what to do if the deploy goes sideways.
    -- Required for high/critical risk per the promote-gate rule below.
    rollback_plan   TEXT,
    -- Approver. For critical risk, two distinct approvers are required
    -- — we model that with a second column rather than a JSONB array
    -- so the FK keeps integrity and the eligibility check is a SELECT.
    approver_id     UUID REFERENCES "user"(id) ON DELETE SET NULL,
    second_approver_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    approved_at     TIMESTAMPTZ,
    promoted_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (environment_id, target_sha)
);

CREATE INDEX idx_deploy_preflight_workspace
    ON deploy_preflight(workspace_id, created_at DESC);
CREATE INDEX idx_deploy_preflight_env
    ON deploy_preflight(environment_id, created_at DESC);

-- 3. Post-deploy health snapshot -----------------------------------------

CREATE TABLE deploy_health_snapshot (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id                UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    deploy_id                   UUID NOT NULL REFERENCES deploy(id) ON DELETE CASCADE,
    snapshot_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Baselines come from the 24 hours BEFORE the deploy completed_at;
    -- "current" comes from the 5 minutes leading up to snapshot_at.
    -- Stored as nullable double precisions because a brand-new
    -- workspace with no historical task_usage data has no baseline to
    -- compute; the live panel renders "—" in that case.
    error_rate_baseline         DOUBLE PRECISION,
    error_rate_current          DOUBLE PRECISION,
    p99_latency_baseline_ms     DOUBLE PRECISION,
    p99_latency_current_ms      DOUBLE PRECISION,
    -- Inbox issues opened since this deploy completed_at. Cumulative
    -- from the deploy completion timestamp, not just this snapshot
    -- window — that's what the UI wants to render.
    inbox_issues_since          INTEGER NOT NULL DEFAULT 0,
    -- Δ between current agent failure rate and the same 24h baseline.
    -- Positive means the deploy made things worse.
    agent_failure_rate_delta    DOUBLE PRECISION
);

-- The most-common read is "give me the latest snapshot for this deploy"
-- — pivot on deploy_id then descend by snapshot_at.
CREATE INDEX idx_deploy_health_deploy
    ON deploy_health_snapshot(deploy_id, snapshot_at DESC);

-- Workspace-wide "any deploy degraded in the last hour?" — partial on
-- snapshot_at to keep the index narrow.
CREATE INDEX idx_deploy_health_workspace
    ON deploy_health_snapshot(workspace_id, snapshot_at DESC);
