-- Skill observation queries — self-learning observation recording and retrieval.
-- Part of TECH-3077 skill library unlock.

-- name: InsertSkillObservation :one
INSERT INTO skill_observation
    (skill_id, skill_version, run_id, autopilot_id, delta_type, delta_value, confidence, outcome)
VALUES
    (@skill_id, @skill_version, @run_id, @autopilot_id, @delta_type, @delta_value, @confidence, @outcome)
RETURNING id, created_at;

-- name: UpsertObservationAggregate :exec
INSERT INTO skill_observation_aggregate
    (skill_id, delta_type, delta_value, run_count, success_count, consistency_pct, last_seen, first_seen)
VALUES
    (@skill_id, @delta_type, @delta_value, 1,
     CASE WHEN @outcome::text = 'success' THEN 1 ELSE 0 END,
     CASE WHEN @outcome::text = 'success' THEN 1.0 ELSE 0.0 END,
     now(), now())
ON CONFLICT (skill_id, delta_type, delta_value) DO UPDATE SET
    run_count = skill_observation_aggregate.run_count + 1,
    success_count = skill_observation_aggregate.success_count +
                    CASE WHEN @outcome::text = 'success' THEN 1 ELSE 0 END,
    consistency_pct = (skill_observation_aggregate.success_count +
                       CASE WHEN @outcome::text = 'success' THEN 1 ELSE 0 END)::float /
                      (skill_observation_aggregate.run_count + 1)::float,
    last_seen = now();

-- name: ListSkillObservations :many
SELECT id, skill_id, skill_version, run_id, autopilot_id,
       delta_type, delta_value, confidence, outcome, created_at
FROM skill_observation
WHERE skill_id = @skill_id
ORDER BY created_at DESC
LIMIT @lim;

-- name: ListObservationAggregates :many
SELECT skill_id, delta_type, delta_value,
       run_count, success_count, consistency_pct,
       last_seen, first_seen, auto_proposed_at
FROM skill_observation_aggregate
WHERE skill_id = @skill_id
ORDER BY run_count DESC, consistency_pct DESC;

-- name: ListAggregatesAboveThreshold :many
-- Returns aggregates that have hit the learning threshold and not yet had
-- a change request generated.
SELECT s.id AS skill_id, s.name AS skill_name, s.workspace_id,
       a.delta_type, a.delta_value, a.run_count, a.consistency_pct
FROM skill_observation_aggregate a
JOIN skill s ON s.id = a.skill_id
WHERE a.run_count >= 10
  AND a.consistency_pct >= 0.70
  AND a.auto_proposed_at IS NULL
ORDER BY a.run_count DESC;

-- name: MarkAggregateProposed :exec
UPDATE skill_observation_aggregate
SET auto_proposed_at = now()
WHERE skill_id = @skill_id
  AND delta_type = @delta_type
  AND delta_value = @delta_value;
