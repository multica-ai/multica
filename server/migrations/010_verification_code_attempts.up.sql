-- CEREBRO-PATCH(migration-idempotent-010-verification-code-attempts): cerebro modification of upstream file
ALTER TABLE verification_code ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
