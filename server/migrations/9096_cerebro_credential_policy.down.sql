-- Rollback 9096_cerebro_credential_policy.
DROP INDEX IF EXISTS idx_cerebro_credential_policy_subject;
DROP INDEX IF EXISTS idx_cerebro_credential_policy_lookup;
DROP TABLE IF EXISTS cerebro_credential_policy;
