DROP INDEX IF EXISTS idx_cerebro_credential_audit_result;

ALTER TABLE cerebro_credential_audit
    DROP CONSTRAINT IF EXISTS cerebro_credential_audit_action_check;

ALTER TABLE cerebro_credential_audit
    ADD CONSTRAINT cerebro_credential_audit_action_check CHECK (action IN (
        'create', 'update', 'delete', 'reveal', 'rotate', 'bind', 'unbind'
    ));

ALTER TABLE cerebro_credential_audit
    DROP COLUMN IF EXISTS reason;

ALTER TABLE cerebro_credential_audit
    DROP COLUMN IF EXISTS result;
