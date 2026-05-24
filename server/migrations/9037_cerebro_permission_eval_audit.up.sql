-- FIR-2130: index permission-engine evaluation audit events for the phase 5 access page.
CREATE INDEX IF NOT EXISTS idx_activity_log_permission_policy_eval
    ON activity_log (workspace_id, created_at DESC)
    WHERE action = 'permission_policy_evaluated';
