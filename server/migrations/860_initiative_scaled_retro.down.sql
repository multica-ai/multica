-- Rolling back removes the scaled rows, since the older checks would refuse them.
DELETE FROM retro_trigger WHERE trigger_kind = 'initiative_scaled';
DELETE FROM initiative_stage_event WHERE kind = 'scaled';

ALTER TABLE retro_trigger DROP CONSTRAINT IF EXISTS retro_trigger_trigger_kind_check;
ALTER TABLE retro_trigger ADD CONSTRAINT retro_trigger_trigger_kind_check
    CHECK (trigger_kind IN ('initiative_closed', 'gate_rejected', 'agent_failures',
        'squad_rejections', 'budget_hold', 'stage_sla')) NOT VALID;
ALTER TABLE retro_trigger VALIDATE CONSTRAINT retro_trigger_trigger_kind_check;

ALTER TABLE initiative_stage_event DROP CONSTRAINT IF EXISTS initiative_stage_event_kind_check;
ALTER TABLE initiative_stage_event ADD CONSTRAINT initiative_stage_event_kind_check
    CHECK (kind IN ('created', 'advanced', 'sent_back', 'shelved',
        'resumed', 'closed', 'forced', 'attested', 'artifact_waived',
        'artifact_linked', 'gate_days_changed')) NOT VALID;
ALTER TABLE initiative_stage_event VALIDATE CONSTRAINT initiative_stage_event_kind_check;
