-- JEH-1197 — Policy-enforcement audit extensions for cerebro_credential_audit.
--
-- Adds result (allow/deny) and reason columns so policy decisions on the five
-- governance actions (attach, read_redacted, reveal, rotate, revoke) leave a
-- structured trail. Existing rows backfill as result='allow' because the only
-- writes today happen after the action has succeeded.
--
-- Also relaxes the action CHECK constraint to include 'read' — used only when
-- a List/Get/ListBindings/ListAudit call is denied by policy. Allows on read
-- are not written; the cost of one row per page-render is too high relative
-- to the value of a positive trail.

ALTER TABLE cerebro_credential_audit
    ADD COLUMN IF NOT EXISTS result TEXT NOT NULL DEFAULT 'allow'
        CHECK (result IN ('allow', 'deny'));

ALTER TABLE cerebro_credential_audit
    ADD COLUMN IF NOT EXISTS reason TEXT NOT NULL DEFAULT '';

ALTER TABLE cerebro_credential_audit
    DROP CONSTRAINT IF EXISTS cerebro_credential_audit_action_check;

ALTER TABLE cerebro_credential_audit
    ADD CONSTRAINT cerebro_credential_audit_action_check CHECK (action IN (
        'create', 'update', 'delete', 'reveal', 'rotate', 'bind', 'unbind', 'read'
    ));

CREATE INDEX IF NOT EXISTS idx_cerebro_credential_audit_result
    ON cerebro_credential_audit (credential_id, result, created_at DESC)
    WHERE result = 'deny';
