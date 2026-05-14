-- Reverses 074. Safe only when no NULL issue_id rows are present.
-- In practice this rollback path is only used during local
-- development, before any production webhook has landed.

BEGIN;

ALTER TABLE cascade_retrigger
    ALTER COLUMN issue_id SET NOT NULL;

COMMIT;
