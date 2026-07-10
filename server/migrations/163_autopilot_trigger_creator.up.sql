-- Human Attribution — per-trigger creator (MUL-4302; Bohan's refinement of the
-- autopilot attribution rule). An autopilot run must be accountable to the human
-- who created the SPECIFIC trigger that fired it — the member who set up the
-- schedule, or who registered the webhook — not the publisher of the whole rule
-- (the previous rule_owner behavior). autopilot_trigger did not record a creator,
-- so this adds one. The resolver reads it as source=trigger_owner (accountable =
-- this member, originator NULL because a scheduled/webhook fire carries no human
-- authorization at fire time — same authz-safe divergence as rule_owner).
--
-- Constraints (MUL-4302 §7 + workspace DB rules): NO foreign key, NO cascade
-- (integrity is app-layer), nullable with no default so the ALTER is a fast
-- metadata-only change (no table rewrite). NULL means "no creator recorded":
-- every trigger that predates this migration, which the resolver degrades to the
-- old rule_owner behavior (then owner_fallback) rather than fabricating a human.
-- created_by_type is 'member' | 'agent' (mirrors autopilot.created_by_type); only
-- a 'member' creator becomes the accountable human, an 'agent'-created trigger
-- degrades to rule_owner like any other agent action.
ALTER TABLE autopilot_trigger
    ADD COLUMN created_by_type TEXT NULL;

ALTER TABLE autopilot_trigger
    ADD COLUMN created_by_id UUID NULL;

COMMENT ON COLUMN autopilot_trigger.created_by_type IS
    'Actor type that created this trigger: member | agent. Paired with created_by_id. Consumed only for attribution (source=trigger_owner) — never authorization. NULL on pre-migration triggers (MUL-4302).';

COMMENT ON COLUMN autopilot_trigger.created_by_id IS
    'The member/agent that created this trigger. For a member creator this is the accountable human of runs this trigger fires (source=trigger_owner). No FK, app-layer integrity. NULL on pre-migration triggers, which degrade to rule_owner (MUL-4302).';
