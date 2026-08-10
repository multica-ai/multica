-- CEREBRO-PATCH(issue-on-behalf-of-column): FIR-4930 — drop the explicit human origin column.
DROP INDEX IF EXISTS idx_issue_on_behalf_of_user_id;

ALTER TABLE issue
    DROP COLUMN IF EXISTS on_behalf_of_user_id;
