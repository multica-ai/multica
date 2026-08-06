ALTER TABLE cerebro_access_decision_ledger
    DROP CONSTRAINT IF EXISTS cerebro_access_decision_reason_code_not_blank;

ALTER TABLE cerebro_access_decision_ledger
    DROP COLUMN IF EXISTS reason_code;
