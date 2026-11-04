-- A brief's scope (spec 4.8): the company's brief, or a floor's brief written
-- by the floor's chief. Existing briefs are company briefs. scope_id names the
-- floor for a floor brief and is NULL for a company brief.
--
-- The CHECK is added NOT VALID and validated after, as in 782, so the table is
-- checked without blocking writes.
ALTER TABLE brief ADD COLUMN IF NOT EXISTS scope_type TEXT NOT NULL DEFAULT 'company';
ALTER TABLE brief ADD COLUMN IF NOT EXISTS scope_id UUID;
ALTER TABLE brief DROP CONSTRAINT IF EXISTS brief_scope_check;
ALTER TABLE brief ADD CONSTRAINT brief_scope_check
    CHECK ((scope_type = 'company' AND scope_id IS NULL)
        OR (scope_type = 'floor' AND scope_id IS NOT NULL)) NOT VALID;
ALTER TABLE brief VALIDATE CONSTRAINT brief_scope_check;
