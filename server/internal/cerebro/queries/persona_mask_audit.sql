-- JEH-1173: persona-mask audit ledger.

-- name: InsertPersonaMaskAudit :exec
INSERT INTO cerebro_persona_mask_audit (
    workspace_id, actor_type, actor_id, action,
    resource_kind, resource_id, classification, decision,
    masked_fields, decision_id, reason
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
);

-- name: ListPersonaMaskAuditByWorkspace :many
SELECT id, workspace_id, actor_type, actor_id, action,
       resource_kind, resource_id, classification, decision,
       masked_fields, decision_id, reason, created_at
FROM cerebro_persona_mask_audit
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListPersonaMaskAuditByResource :many
SELECT id, workspace_id, actor_type, actor_id, action,
       resource_kind, resource_id, classification, decision,
       masked_fields, decision_id, reason, created_at
FROM cerebro_persona_mask_audit
WHERE resource_kind = $1 AND resource_id = $2
ORDER BY created_at DESC
LIMIT $3;
