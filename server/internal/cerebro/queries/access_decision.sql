-- name: AppendCerebroAccessDecisionLedger :exec
INSERT INTO cerebro_access_decision_ledger (
    workspace_id, agent_id, runtime_id, on_behalf_of_user_id, task_id, issue_id,
    observed_tool_name, canonical_capability_id, legacy_decision, legacy_path,
    shadow_decision, policy_decision, evidence_level, differs, reason
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14, $15
);

-- name: ReportCerebroAccessDecisionLedger :many
SELECT
    agent_id,
    runtime_id,
    COALESCE(canonical_capability_id, observed_tool_name) AS tool,
    CASE
        WHEN COUNT(DISTINCT policy_decision) = 1 THEN MIN(policy_decision)
        ELSE ''::text
    END AS policy_decision,
    COUNT(*)::bigint AS total,
    COUNT(*) FILTER (WHERE differs)::bigint AS diffs
FROM cerebro_access_decision_ledger
WHERE workspace_id = $1
GROUP BY agent_id, runtime_id, COALESCE(canonical_capability_id, observed_tool_name)
ORDER BY agent_id, runtime_id, tool;
