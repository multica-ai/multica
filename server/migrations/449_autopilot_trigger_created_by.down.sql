-- Reverting drops the immutable creator. Dispatch then falls back to the
-- pre-MUL-6951 resolution, which reads published_by, so restore migration 189's
-- original column comments verbatim rather than leaving MUL-6951 text describing
-- a model this database no longer runs.
COMMENT ON COLUMN autopilot_trigger.published_by_type IS
    'Actor type of the trigger''s current responsible publisher: member | agent. Set to the creator at creation and re-stamped to the editor on any substantive edit governing this trigger. Consumed only for attribution (source=trigger_owner) — never authorization. NULL on pre-migration triggers (MUL-4302).';

COMMENT ON COLUMN autopilot_trigger.published_by_id IS
    'The member/agent currently responsible for this trigger''s effective config (creator, then last substantive editor). For a member this is the accountable human of runs the trigger fires (source=trigger_owner). No FK, app-layer integrity. NULL on pre-migration triggers, which degrade to rule_owner (MUL-4302).';

ALTER TABLE autopilot_trigger DROP COLUMN IF EXISTS created_by_id;
ALTER TABLE autopilot_trigger DROP COLUMN IF EXISTS created_by_type;
