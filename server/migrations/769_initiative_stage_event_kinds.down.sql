-- Rows of the two new kinds cannot survive the old check, so they are removed
-- with it. Only this down migration removes timeline rows.
DELETE FROM initiative_stage_event WHERE kind IN ('artifact_linked', 'gate_days_changed');
ALTER TABLE initiative_stage_event DROP CONSTRAINT IF EXISTS initiative_stage_event_kind_check;
ALTER TABLE initiative_stage_event ADD CONSTRAINT initiative_stage_event_kind_check
    CHECK (kind IN ('created', 'advanced', 'sent_back', 'shelved',
        'resumed', 'closed', 'forced', 'attested', 'artifact_waived')) NOT VALID;
ALTER TABLE initiative_stage_event VALIDATE CONSTRAINT initiative_stage_event_kind_check;
