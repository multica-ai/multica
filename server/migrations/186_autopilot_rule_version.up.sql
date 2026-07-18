-- Human Attribution, Phase 1 — autopilot rule_owner snapshot table (MUL-4302 §3.4/§7).
--
-- An autopilot trigger (schedule / webhook / manual) enqueues a run that NO human
-- authorized in the moment, so its authorization originator_user_id stays NULL. But
-- the run is still accountable to a human for audit / cost: the member who last
-- PUBLISHED the rule that fired it. "Publish" = a substantive action (create, enable,
-- resume, or a change to the trigger condition / execution target / task
-- instructions); cosmetic edits (rename, description) do NOT transfer accountability.
--
-- Each substantive publish appends one immutable row here, recording who published
-- and a summary of the effective config at that moment. At dispatch time the resolver
-- reads the autopilot's ACTIVE (latest) version and stamps the run:
-- originator_source='rule_owner', accountable_user_id=published_by_id, and
-- agent_task_queue.rule_version_id=<this row>. Authorization is untouched — this is
-- the audit side of the accountable/originator split.
--
-- Immutable + append-only: a new publish never UPDATEs a prior row, so a run's
-- rule_version_id always resolves to the exact config/publisher that was live when it
-- fired, even after the rule is later re-published or the publisher leaves.
--
-- Constraints (MUL-4302 §7 + workspace DB rules): NO foreign key, NO cascade —
-- integrity is resolved in the application layer. published_by_id is nullable
-- (system-published rules have no member); a NULL publisher degrades the run to
-- 'unattributed' rather than fabricating a human.
CREATE TABLE IF NOT EXISTS autopilot_rule_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    autopilot_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    published_by_type TEXT NOT NULL,
    published_by_id UUID NULL,
    config_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Application-layer integrity only: proactively drop any *_fkey a tool might add,
-- matching the workspace no-FK / no-cascade rule (see 152_chat_pinned_agent).
ALTER TABLE autopilot_rule_version
    DROP CONSTRAINT IF EXISTS autopilot_rule_version_autopilot_id_fkey,
    DROP CONSTRAINT IF EXISTS autopilot_rule_version_workspace_id_fkey,
    DROP CONSTRAINT IF EXISTS autopilot_rule_version_published_by_id_fkey;

COMMENT ON TABLE autopilot_rule_version IS
    'Append-only snapshot of autopilot rule publishes (MUL-4302 §3.4). One row per substantive publish (create / enable / resume / trigger-condition / target / instructions change), recording the publisher + effective-config summary. Dispatch resolves the latest row for an autopilot as the run''s rule_owner accountable human. No FK, no cascade.';
