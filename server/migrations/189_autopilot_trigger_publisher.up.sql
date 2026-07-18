-- Human Attribution — per-trigger responsible publisher (MUL-4302; Bohan + Elon).
-- An autopilot schedule/webhook run is accountable to the human RESPONSIBLE for the
-- firing trigger's current effective configuration — initially whoever created the
-- trigger, then TRANSFERRED to whoever later makes a substantive edit that governs
-- it (the trigger's own cron / filter / enabled state, or an autopilot-level
-- target / instructions / enable change that affects all of its triggers). This models
-- the "current effective automation version's responsible publisher" resolved PER
-- firing trigger, so editing one trigger never reassigns responsibility for another.
--
-- Stored on the trigger row (not derived from the autopilot-scoped rule_version)
-- precisely so per-trigger granularity holds. The resolver reads it as
-- source=trigger_owner (accountable = this member, originator NULL — a scheduled /
-- webhook fire carries no human authorization at fire time; the same authz-safe
-- divergence as rule_owner).
--
-- Constraints (MUL-4302 §7 + workspace DB rules): NO foreign key, NO cascade
-- (integrity is app-layer), nullable with no default so the ALTER is a fast
-- metadata-only change. published_by_type is 'member' | 'agent'; only a 'member'
-- publisher becomes the accountable human — an 'agent'-published trigger degrades to
-- rule_owner like any other agent action. NULL means no publisher recorded (a
-- trigger predating this migration), which degrades to rule_owner then owner_fallback.
ALTER TABLE autopilot_trigger
    ADD COLUMN published_by_type TEXT NULL;

ALTER TABLE autopilot_trigger
    ADD COLUMN published_by_id UUID NULL;

COMMENT ON COLUMN autopilot_trigger.published_by_type IS
    'Actor type of the trigger''s current responsible publisher: member | agent. Set to the creator at creation and re-stamped to the editor on any substantive edit governing this trigger. Consumed only for attribution (source=trigger_owner) — never authorization. NULL on pre-migration triggers (MUL-4302).';

COMMENT ON COLUMN autopilot_trigger.published_by_id IS
    'The member/agent currently responsible for this trigger''s effective config (creator, then last substantive editor). For a member this is the accountable human of runs the trigger fires (source=trigger_owner). No FK, app-layer integrity. NULL on pre-migration triggers, which degrade to rule_owner (MUL-4302).';
