-- name: AppendCerebroAccessDecisionLedger :exec
INSERT INTO cerebro_access_decision_ledger (
    workspace_id, agent_id, runtime_id, on_behalf_of_user_id, task_id, issue_id,
    observed_tool_name, canonical_capability_id, legacy_decision, legacy_path,
    shadow_decision, policy_decision, evidence_level, differs, reason_code, reason
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16
);

-- name: ListTaskAccessDecisionDiagnostics :many
SELECT observed_tool_name,
       COALESCE(canonical_capability_id, '') AS canonical_capability_id,
       legacy_decision AS decision,
       policy_decision,
       legacy_path,
       reason_code,
       reason,
       created_at
FROM cerebro_access_decision_ledger
WHERE task_id = $1
  AND legacy_decision = 'deny'
ORDER BY created_at DESC
LIMIT 50;
