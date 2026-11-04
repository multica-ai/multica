-- Scaling an initiative at an operate review starts a retrospective, as closing
-- one does (spec 3.6: Learn runs when an initiative is sunset, scaled or closed).
-- Scaling leaves the initiative operating, so it has its own timeline kind,
-- scaled, and its own retrospective trigger, initiative_scaled.
--
-- Both checks are added NOT VALID and validated after, so the tables are checked
-- without blocking writes while the constraints are being validated.
ALTER TABLE initiative_stage_event DROP CONSTRAINT IF EXISTS initiative_stage_event_kind_check;
ALTER TABLE initiative_stage_event ADD CONSTRAINT initiative_stage_event_kind_check
    CHECK (kind IN ('created', 'advanced', 'sent_back', 'shelved',
        'resumed', 'closed', 'forced', 'attested', 'artifact_waived',
        'artifact_linked', 'gate_days_changed', 'scaled')) NOT VALID;
ALTER TABLE initiative_stage_event VALIDATE CONSTRAINT initiative_stage_event_kind_check;

ALTER TABLE retro_trigger DROP CONSTRAINT IF EXISTS retro_trigger_trigger_kind_check;
ALTER TABLE retro_trigger ADD CONSTRAINT retro_trigger_trigger_kind_check
    CHECK (trigger_kind IN ('initiative_closed', 'gate_rejected', 'agent_failures',
        'squad_rejections', 'budget_hold', 'stage_sla', 'initiative_scaled')) NOT VALID;
ALTER TABLE retro_trigger VALIDATE CONSTRAINT retro_trigger_trigger_kind_check;
