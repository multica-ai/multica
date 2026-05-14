BEGIN;

ALTER TABLE cascade_retrigger
    DROP COLUMN IF EXISTS branch,
    DROP COLUMN IF EXISTS pr_title;

COMMIT;
