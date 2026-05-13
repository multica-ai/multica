DROP INDEX IF EXISTS idx_cerebro_credential_audit_workspace;
DROP INDEX IF EXISTS idx_cerebro_credential_audit_credential;
DROP TABLE IF EXISTS cerebro_credential_audit;

DROP INDEX IF EXISTS idx_cerebro_credential_binding_resource;
DROP INDEX IF EXISTS idx_cerebro_credential_binding_credential;
DROP TABLE IF EXISTS cerebro_credential_binding;

DROP INDEX IF EXISTS idx_cerebro_credential_expires_at;
DROP INDEX IF EXISTS idx_cerebro_credential_workspace;
DROP TABLE IF EXISTS cerebro_credential;
