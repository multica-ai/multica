-- Permission evaluation audit (JEH-978).
--
-- The cerebro_workspace_grant control plane was retired in FIR-1512 (the table
-- and its CRUD/read queries are gone). The only query left here writes the
-- per-evaluation audit row, which lands in activity_log — a different table
-- that is NOT dropped.

-- name: InsertCerebroPermissionAuditEvent :exec
INSERT INTO activity_log (
    workspace_id,
    actor_type,
    actor_id,
    action,
    details
) VALUES (
    $1,
    $2,
    $3,
    'permission_policy_evaluated',
    $4
);
