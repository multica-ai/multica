-- FIR-3212: satisfaction signals join the existing quality measurement model
-- instead of getting a parallel pipe. A 'satisfaction' measurement records a
-- documented human behavior signal (a member reaction on an agent comment, or
-- a human approval/rejection of a gate round) attributed to the run that
-- produced the work. Verdicts are the raw signal (emoji, approved/rejected) —
-- interpretation happens at aggregation time, never at capture.
ALTER TABLE cerebro_analytics_quality_measurement
    DROP CONSTRAINT IF EXISTS cerebro_analytics_quality_measurement_measurement_type_check;
ALTER TABLE cerebro_analytics_quality_measurement
    ADD CONSTRAINT cerebro_analytics_quality_measurement_measurement_type_check
    CHECK (measurement_type IN ('judge_gate', 'skill_observation', 'evaluator', 'satisfaction'));
