-- FIR-3212: quality and satisfaction aggregated per agent config version.
--
-- The sample base is cerebro_run_prompt_snapshot — only runs that recorded
-- which config version they actually read can be attributed to a version, so
-- before/after comparisons never mix versions. Measurements arrive through the
-- analytics projection (cerebro_analytics_quality_measurement): solution
-- quality is judge_gate / skill_observation / evaluator verdicts; satisfaction
-- is member reactions and human approval/rejection decisions. Everything is
-- reported as counts and sums with explicit sample sizes — never a percentage
-- — so the reader (Go handler) can only form an average when the sample size
-- is non-zero and missing data reads as missing, not as zero.

-- name: AggregateAgentQualityByVersion :many
SELECT
    ps.agent_context_version,
    ps.agent_context_version_id,
    COUNT(DISTINCT ps.task_id)::bigint AS runs,
    MIN(ps.created_at)::timestamptz AS first_run_at,
    MAX(ps.created_at)::timestamptz AS last_run_at,
    COUNT(DISTINCT ar.id) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator'))::bigint AS solution_measured_runs,
    COUNT(qm.id) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator'))::bigint AS solution_measurements,
    COUNT(qm.id) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator') AND qm.verdict IN ('pass','success'))::bigint AS solution_passes,
    COUNT(qm.score) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator'))::bigint AS solution_scored,
    COALESCE(SUM(qm.score) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator')), 0)::float8 AS solution_score_sum,
    COUNT(qm.confidence) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator'))::bigint AS solution_confident,
    COALESCE(SUM(qm.confidence) FILTER (WHERE qm.measurement_type IN ('judge_gate','skill_observation','evaluator')), 0)::float8 AS solution_confidence_sum,
    COUNT(DISTINCT ar.id) FILTER (WHERE qm.measurement_type = 'satisfaction')::bigint AS satisfaction_measured_runs,
    COUNT(qm.id) FILTER (WHERE qm.measurement_type = 'satisfaction' AND qm.category LIKE 'reaction:%')::bigint AS reactions,
    COUNT(qm.id) FILTER (WHERE qm.measurement_type = 'satisfaction' AND qm.verdict = 'approved')::bigint AS approvals,
    COUNT(qm.id) FILTER (WHERE qm.measurement_type = 'satisfaction' AND qm.verdict = 'rejected')::bigint AS rejections
FROM cerebro_run_prompt_snapshot ps
LEFT JOIN cerebro_analytics_run ar ON ar.run_id = ps.task_id AND ar.workspace_id = ps.workspace_id
LEFT JOIN cerebro_analytics_quality_measurement qm ON qm.analytics_run_id = ar.id
WHERE ps.agent_id = $1
GROUP BY ps.agent_context_version, ps.agent_context_version_id
ORDER BY MAX(ps.created_at) DESC;
