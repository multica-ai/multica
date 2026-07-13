-- Same rationale as migrations 158/159: the UPDATE only fills NULL
-- values and cannot be reversed without an audit table. Down is a
-- no-op; use the manual soft-rollback SQL from migration 158 if
-- remediation is needed.
SELECT 1;