-- Reverses 073. Safe only if no NULL issue_id rows exist; in practice
-- this is run by a developer rolling back PR3 locally before any
-- production webhook landed, so the constraint will hold.

BEGIN;

ALTER TABLE cascade_retrigger
    ALTER COLUMN issue_id SET NOT NULL;

COMMIT;
