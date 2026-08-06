-- 9169_cerebro_access_decision_reason_code: classify Decision Ledger evidence
-- with a machine-readable code while keeping reason as display-only copy.

ALTER TABLE cerebro_access_decision_ledger
    ADD COLUMN IF NOT EXISTS reason_code TEXT NOT NULL DEFAULT 'legacy_unknown';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cerebro_access_decision_reason_code_not_blank'
          AND conrelid = 'cerebro_access_decision_ledger'::regclass
    ) THEN
        ALTER TABLE cerebro_access_decision_ledger
            ADD CONSTRAINT cerebro_access_decision_reason_code_not_blank
            CHECK (length(trim(reason_code)) > 0);
    END IF;
END
$$;
