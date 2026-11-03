-- The initiative timeline gains two kinds (spec 3.1: every write appends an
-- event). artifact_linked is a published document taking a required artifact of
-- the current stage. gate_days_changed is a change to the operate review days.
--
-- The check is added NOT VALID and validated after, so the table is checked
-- without blocking writes while the constraint is being validated.
ALTER TABLE initiative_stage_event DROP CONSTRAINT IF EXISTS initiative_stage_event_kind_check;
ALTER TABLE initiative_stage_event ADD CONSTRAINT initiative_stage_event_kind_check
    CHECK (kind IN ('created', 'advanced', 'sent_back', 'shelved',
        'resumed', 'closed', 'forced', 'attested', 'artifact_waived',
        'artifact_linked', 'gate_days_changed')) NOT VALID;
ALTER TABLE initiative_stage_event VALIDATE CONSTRAINT initiative_stage_event_kind_check;
